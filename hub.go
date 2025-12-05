package websocket

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"sync"
	"sync/atomic"

	"github.com/pogo/websocket/internal/protocol"
	"go.uber.org/zap"
)

const (
	NumShards      = 32
	MaxConnections = 10000
)

var (
	hubRegistry sync.Map
)

func RegisterHub(appID string, h *Hub) {
	hubRegistry.Store(appID, h)
}

func UnregisterHub(appID string) {
	hubRegistry.Delete(appID)
}

func GetHub(appID string) *Hub {
	if val, ok := hubRegistry.Load(appID); ok {
		return val.(*Hub)
	}
	return nil
}

type Hub struct {
	AppID   string
	auth    AuthProvider
	broker  Broker
	logger  *zap.Logger
	metrics *Metrics
	ctx     context.Context
	shards  []*HubShard

	// Synchronization
	conns atomic.Int64
	wg    sync.WaitGroup
	done  chan struct{}

	clientMessage chan *ClientMessageWrapper
	subscribe     chan *Subscription
	unsubscribe   chan *Subscription
}

type BroadcastMessage struct {
	Channel string `json:"channel"`
	Event   string `json:"event"`
	Data    string `json:"data"`
}

type Subscription struct {
	Client   *Client
	Channel  string
	AuthData json.RawMessage
}

type ClientMessageWrapper struct {
	Client  *Client
	Channel string
	Event   string
	Data    json.RawMessage
}

func NewHub(appID string, logger *zap.Logger, ctx context.Context, metrics *Metrics, auth AuthProvider, webhook *WebhookManager, broker Broker) *Hub {
	h := &Hub{
		AppID:         appID,
		auth:          auth,
		broker:        broker,
		logger:        logger,
		metrics:       metrics,
		ctx:           ctx,
		done:          make(chan struct{}),
		clientMessage: make(chan *ClientMessageWrapper),
		subscribe:     make(chan *Subscription),
		unsubscribe:   make(chan *Subscription),
		shards:        make([]*HubShard, NumShards),
	}

	for i := 0; i < NumShards; i++ {
		h.shards[i] = NewHubShard(i, logger, ctx, metrics, webhook)
		go h.shards[i].Run()
	}

	return h
}

func (h *Hub) getShard(key string) *HubShard {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	idx := hash.Sum32() % uint32(NumShards)
	return h.shards[idx]
}

func (h *Hub) Register(c *Client) {
	if h.conns.Load() >= MaxConnections {
		h.logger.Warn("Hub: max connections reached, rejecting", zap.String("id", c.ID))
		c.conn.Close()
		return
	}

	h.conns.Add(1)
	h.wg.Add(1)
	h.metrics.Connections.Inc()

	shard := h.getShard(c.ID)

	select {
	case shard.register <- c:
		h.logger.Debug("Hub: registered client", zap.String("id", c.ID))
	case <-h.ctx.Done():
		h.conns.Add(-1)
		h.wg.Done()
		h.metrics.Connections.Dec()
		c.conn.Close()
	}
}

func (h *Hub) Unregister(c *Client) {
	h.conns.Add(-1)
	h.metrics.Connections.Dec()

	for _, shard := range h.shards {
		select {
		case shard.remove <- c:
		case <-h.ctx.Done():
		}
	}

	h.wg.Done()
}

func (h *Hub) Wait() {
	<-h.done
}

func (h *Hub) Authorize(client *Client, channel string) AuthResult {
	if len(channel) > protocol.MaxChannelLength {
		return AuthResult{Allowed: false}
	}
	return h.auth.Authorize(client, channel)
}

func (h *Hub) Publish(channel, event, data string) bool {
	if len(channel) > protocol.MaxChannelLength || len(event) > protocol.MaxEventLength || len(data) > protocol.MaxDataSize {
		h.logger.Warn("Hub: dropped oversized message")
		return false
	}

	h.metrics.Messages.Inc()

	msg := &BroadcastMessage{
		Channel: channel,
		Event:   event,
		Data:    data,
	}

	err := h.broker.Publish(h.ctx, msg)
	if err != nil {
		h.logger.Error("Hub: broker publish failed", zap.Error(err))
		return false
	}
	return true
}

func (h *Hub) Run() {
	defer close(h.done)
	h.logger.Info("Hub: started", zap.String("app_id", h.AppID))

	brokerStream, err := h.broker.Subscribe(h.ctx)
	if err != nil {
		h.logger.Fatal("Hub: failed to subscribe to broker", zap.Error(err))
	}

	for {
		select {
		case msg := <-brokerStream:
			if msg != nil {
				shard := h.getShard(msg.Channel)
				shard.broadcast <- msg
			}

		case sub := <-h.subscribe:
			if len(sub.Channel) <= protocol.MaxChannelLength {
				shard := h.getShard(sub.Channel)
				shard.subscribe <- sub
			}

		case sub := <-h.unsubscribe:
			if len(sub.Channel) <= protocol.MaxChannelLength {
				shard := h.getShard(sub.Channel)
				shard.unsubscribe <- sub
			}

		case cMsg := <-h.clientMessage:
			shard := h.getShard(cMsg.Channel)
			shard.clientMsg <- cMsg

		case <-h.ctx.Done():
			h.logger.Info("Hub: shutting down, draining connections...")
			h.broker.Close()
			h.wg.Wait()
			h.logger.Info("Hub: shutdown complete")
			return
		}
	}
}
