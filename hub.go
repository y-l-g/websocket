package websocket

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"sync"

	"go.uber.org/zap"
)

const (
	NumShards        = 32
	MaxChannelLength = 256
	MaxEventLength   = 64
	MaxDataSize      = 256 * 1024

	// Security: Maximum concurrent connections per node
	MaxConnections = 10000
)

var GlobalHub *Hub
var globalHubMu sync.RWMutex

type Hub struct {
	auth   AuthProvider
	broker Broker
	logger *zap.Logger
	ctx    context.Context
	shards []*HubShard

	clientMessage chan *ClientMessageWrapper
	register      chan *Client
	unregister    chan *Client
	subscribe     chan *Subscription
	unsubscribe   chan *Subscription
	clients       map[*Client]bool
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

func NewHub(logger *zap.Logger, ctx context.Context, auth AuthProvider, webhook *WebhookManager, broker Broker) *Hub {
	h := &Hub{
		auth:          auth,
		broker:        broker,
		logger:        logger,
		ctx:           ctx,
		clientMessage: make(chan *ClientMessageWrapper),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		subscribe:     make(chan *Subscription),
		unsubscribe:   make(chan *Subscription),
		clients:       make(map[*Client]bool),
		shards:        make([]*HubShard, NumShards),
	}

	for i := 0; i < NumShards; i++ {
		h.shards[i] = NewHubShard(i, logger, ctx, webhook)
		go h.shards[i].Run()
	}

	return h
}

func (h *Hub) getShard(channel string) *HubShard {
	hash := fnv.New32a()
	hash.Write([]byte(channel))
	idx := hash.Sum32() % uint32(NumShards)
	return h.shards[idx]
}

func (h *Hub) Authorize(client *Client, channel string) AuthResult {
	if len(channel) > MaxChannelLength {
		return AuthResult{Allowed: false}
	}
	return h.auth.Authorize(client, channel)
}

func (h *Hub) Publish(channel, event, data string) bool {
	if len(channel) > MaxChannelLength || len(event) > MaxEventLength || len(data) > MaxDataSize {
		h.logger.Warn("Hub: dropped oversized message")
		return false
	}

	metricMessages.Inc()

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
	h.logger.Info("Hub: started")

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

		case client := <-h.register:
			// --- ENFORCE CONNECTION LIMIT ---
			if len(h.clients) >= MaxConnections {
				h.logger.Warn("Hub: max connections reached, rejecting client", zap.String("id", client.ID))
				close(client.send) // Close immediately
				continue
			}
			// --------------------------------

			h.clients[client] = true
			metricConnections.Inc()
			h.logger.Debug("Hub: client connected", zap.String("id", client.ID))

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				metricConnections.Dec()
				close(client.send)
				for _, shard := range h.shards {
					select {
					case shard.remove <- client:
					default:
					}
				}
			}

		case sub := <-h.subscribe:
			if len(sub.Channel) <= MaxChannelLength {
				shard := h.getShard(sub.Channel)
				shard.subscribe <- sub
			}

		case sub := <-h.unsubscribe:
			if len(sub.Channel) <= MaxChannelLength {
				shard := h.getShard(sub.Channel)
				shard.unsubscribe <- sub
			}

		case cMsg := <-h.clientMessage:
			shard := h.getShard(cMsg.Channel)
			shard.clientMsg <- cMsg

		case <-h.ctx.Done():
			h.broker.Close()
			return
		}
	}
}

func SetGlobalHub(h *Hub) {
	globalHubMu.Lock()
	GlobalHub = h
	globalHubMu.Unlock()
}

func GetGlobalHub() *Hub {
	globalHubMu.RLock()
	defer globalHubMu.RUnlock()
	return GlobalHub
}
