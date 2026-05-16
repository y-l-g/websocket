package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/y-l-g/websocket/module/internal/protocol"
	"go.uber.org/zap"
)

const (
	DefaultOutboundQueueSize  = 256
	DefaultClientMsgRateLimit = 50
	DefaultClientMsgRateBurst = 20
)

var (
	hubRegistry sync.Map
)

type PublishStatus int

const (
	PublishOK PublishStatus = iota
	PublishHubMissing
	PublishChannelTooLong
	PublishEventTooLong
	PublishPayloadTooLarge
	PublishInvalidPayloadJSON
	PublishBrokerFailed
	PublishInvalidChannelsJSON
)

type hubSet struct {
	mu   sync.RWMutex
	hubs map[*Hub]struct{}
}

func RegisterHub(appID string, h *Hub) error {
	actual, loaded := hubRegistry.LoadOrStore(appID, &hubSet{hubs: make(map[*Hub]struct{})})
	set := actual.(*hubSet)

	set.mu.Lock()
	set.hubs[h] = struct{}{}
	count := len(set.hubs)
	set.mu.Unlock()

	if loaded {
		h.logger.Info("Hub: AppID registry has multiple active hubs", zap.String("app_id", appID), zap.Int("active_hubs", count))
	}
	return nil
}

func UnregisterHub(appID string, h *Hub) {
	if val, ok := hubRegistry.Load(appID); ok {
		set := val.(*hubSet)
		set.mu.Lock()
		delete(set.hubs, h)
		set.mu.Unlock()
	}
}

func GetHub(appID string) *Hub {
	hubs := GetHubs(appID)
	if len(hubs) == 0 {
		return nil
	}
	return hubs[0]
}

func GetHubs(appID string) []*Hub {
	if val, ok := hubRegistry.Load(appID); ok {
		set := val.(*hubSet)
		set.mu.RLock()
		defer set.mu.RUnlock()

		hubs := make([]*Hub, 0, len(set.hubs))
		for hub := range set.hubs {
			hubs = append(hubs, hub)
		}
		return hubs
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

	// Config
	maxConnections     int64
	numShards          int
	activityTimeout    int // Seconds
	outboundQueueSize  int
	writeBurstSize     int
	clientMsgRateLimit float64
	clientMsgRateBurst int

	// Synchronization
	clientsMu sync.RWMutex
	clients   map[*Client]bool
	conns     atomic.Int64
	wg        sync.WaitGroup
	done      chan struct{}

	clientMessage chan *ClientMessageWrapper
	subscribe     chan *Subscription
	unsubscribe   chan *Subscription
}

type BroadcastMessage struct {
	Channel           string          `json:"channel"`
	Event             string          `json:"event"`
	Data              json.RawMessage `json:"data"`
	InternalCreatedAt time.Time       `json:"-"`
	BrokerReceivedAt  time.Time       `json:"-"`
	ShardBroadcastAt  time.Time       `json:"-"`
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

type DeliveryConfig struct {
	OutboundQueueSize  int
	WriteBurstSize     int
	ClientMsgRateLimit float64
	ClientMsgRateBurst int
	EnableCompression  bool
}

func DefaultDeliveryConfig() DeliveryConfig {
	return DeliveryConfig{
		OutboundQueueSize:  DefaultOutboundQueueSize,
		WriteBurstSize:     DefaultWriteBurstSize,
		ClientMsgRateLimit: DefaultClientMsgRateLimit,
		ClientMsgRateBurst: DefaultClientMsgRateBurst,
		EnableCompression:  false,
	}
}

func (c DeliveryConfig) withDefaults() DeliveryConfig {
	defaults := DefaultDeliveryConfig()
	if c.OutboundQueueSize <= 0 {
		c.OutboundQueueSize = defaults.OutboundQueueSize
	}
	if c.WriteBurstSize <= 0 {
		c.WriteBurstSize = defaults.WriteBurstSize
	}
	if c.ClientMsgRateLimit <= 0 {
		c.ClientMsgRateLimit = defaults.ClientMsgRateLimit
	}
	if c.ClientMsgRateBurst <= 0 {
		c.ClientMsgRateBurst = defaults.ClientMsgRateBurst
	}
	return c
}

func NewHub(appID string, logger *zap.Logger, ctx context.Context, metrics *Metrics, auth AuthProvider, webhook *WebhookManager, broker Broker, maxConn int, numShards int, pingPeriod time.Duration, delivery DeliveryConfig) *Hub {
	delivery = delivery.withDefaults()
	if numShards <= 0 {
		numShards = 32
	}
	if numShards > 64 {
		numShards = 64
		logger.Warn("num_shards clamped to 64 (max supported)")
	}

	// Calculate activity timeout (seconds) based on ping period
	// Recommended: activity_timeout = 120 (default)
	// Spec: The number of seconds of server inactivity after which the client should initiate a ping message
	timeoutSec := int(pingPeriod.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 120
	}

	h := &Hub{
		AppID:              appID,
		auth:               auth,
		broker:             broker,
		logger:             logger,
		metrics:            metrics,
		ctx:                ctx,
		maxConnections:     int64(maxConn),
		numShards:          numShards,
		activityTimeout:    timeoutSec,
		outboundQueueSize:  delivery.OutboundQueueSize,
		writeBurstSize:     delivery.WriteBurstSize,
		clientMsgRateLimit: delivery.ClientMsgRateLimit,
		clientMsgRateBurst: delivery.ClientMsgRateBurst,
		done:               make(chan struct{}),
		clientMessage:      make(chan *ClientMessageWrapper),
		subscribe:          make(chan *Subscription),
		unsubscribe:        make(chan *Subscription),
		shards:             make([]*HubShard, numShards),
		clients:            make(map[*Client]bool),
	}

	for i := 0; i < numShards; i++ {
		h.shards[i] = NewHubShard(i, logger, ctx, metrics, webhook)
		go h.shards[i].Run()
	}

	return h
}

func (h *Hub) getShard(key string) *HubShard {
	hash := fnv.New32a()
	hash.Write([]byte(key))
	idx := hash.Sum32() % uint32(h.numShards)
	return h.shards[idx]
}

func (h *Hub) Register(c *Client) {
	if h.conns.Load() >= h.maxConnections {
		h.logger.Warn("Hub: max connections reached, rejecting", zap.String("id", c.ID))

		// Protocol Compliance: Send 4100 Over Capacity
		deadline := time.Now().Add(1 * time.Second)
		msg := websocket.FormatCloseMessage(protocol.ErrorOverCapacity, "Over capacity")
		// Best effort write control
		_ = c.conn.WriteControl(websocket.CloseMessage, msg, deadline)
		_ = c.conn.Close()
		return
	}

	h.clientsMu.Lock()
	h.clients[c] = true
	h.clientsMu.Unlock()

	h.conns.Add(1)
	h.wg.Add(1)
	h.metrics.Connections.Inc()

	h.logger.Debug("Hub: registered client", zap.String("id", c.ID))

	dataMap := map[string]interface{}{
		"socket_id":        c.ID,
		"activity_timeout": h.activityTimeout, // Synced with Server Config
	}
	dataBytes, _ := json.Marshal(dataMap)
	payload := map[string]interface{}{
		"event": protocol.EventConnectionEstablished,
		"data":  string(dataBytes),
	}
	msg, _ := json.Marshal(payload)

	c.Send(msg)
}

func (h *Hub) Unregister(c *Client) {
	h.clientsMu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.clientsMu.Unlock()
		return
	}
	delete(h.clients, c)
	h.clientsMu.Unlock()

	h.conns.Add(-1)
	h.metrics.Connections.Dec()

	for i := 0; i < h.numShards; i++ {
		if c.HasShard(i) {
			select {
			case h.shards[i].cleanup <- c:
			case <-h.ctx.Done():
			}
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
	return h.publish(channel, event, data) == PublishOK
}

func (h *Hub) publish(channel, event, data string) PublishStatus {
	totalStart := time.Now()
	validateStart := totalStart
	hotPath := h.metrics != nil && h.metrics.HotPathEnabled

	if len(channel) > protocol.MaxChannelLength {
		h.logger.Error("Hub: publish failed, channel name too long",
			zap.String("channel", channel),
			zap.Int("length", len(channel)),
			zap.Int("limit", protocol.MaxChannelLength))
		return PublishChannelTooLong
	}

	if len(event) > protocol.MaxEventLength {
		h.logger.Error("Hub: publish failed, event name too long",
			zap.String("event", event),
			zap.Int("length", len(event)),
			zap.Int("limit", protocol.MaxEventLength))
		return PublishEventTooLong
	}

	if len(data) > protocol.MaxDataSize {
		h.logger.Error("Hub: publish failed, data payload too large",
			zap.Int("length", len(data)),
			zap.Int("limit", protocol.MaxDataSize))
		return PublishPayloadTooLarge
	}

	if !json.Valid([]byte(data)) {
		h.logger.Error("Hub: publish failed, data payload is not valid JSON")
		return PublishInvalidPayloadJSON
	}

	if hotPath {
		h.metrics.PublishDuration.WithLabelValues("validate").Observe(time.Since(validateStart).Seconds())
		defer func() {
			h.metrics.PublishDuration.WithLabelValues("total").Observe(time.Since(totalStart).Seconds())
		}()
	}

	h.metrics.Messages.Inc()

	raw := json.RawMessage(data)

	msg := &BroadcastMessage{
		Channel:           channel,
		Event:             event,
		Data:              raw,
		InternalCreatedAt: time.Now(),
	}

	brokerStart := time.Now()
	err := h.broker.Publish(h.ctx, msg)
	if hotPath {
		h.metrics.PublishDuration.WithLabelValues("broker").Observe(time.Since(brokerStart).Seconds())
	}
	if err != nil {
		h.logger.Error("Hub: broker publish failed", zap.Error(err))
		return PublishBrokerFailed
	}
	return PublishOK
}

func (h *Hub) publishScope() string {
	if scoped, ok := h.broker.(interface{ PublishScope() string }); ok {
		return scoped.PublishScope()
	}
	return fmt.Sprintf("hub:%p", h)
}

func publishToActiveHubs(hubs []*Hub, channel, event, data string) PublishStatus {
	if len(hubs) == 0 {
		return PublishHubMissing
	}

	publishedScopes := make(map[string]struct{}, len(hubs))
	status := PublishOK
	for _, hub := range hubs {
		scope := hub.publishScope()
		if _, seen := publishedScopes[scope]; seen {
			continue
		}
		publishedScopes[scope] = struct{}{}

		if result := hub.publish(channel, event, data); result != PublishOK && status == PublishOK {
			status = result
		}
	}
	return status
}

func (h *Hub) Run() {
	defer close(h.done)
	h.logger.Info("Hub: started", zap.String("app_id", h.AppID), zap.Int("shards", h.numShards))

	brokerStream, err := h.broker.Subscribe(h.ctx)
	if err != nil {
		h.logger.Fatal("Hub: failed to subscribe to broker", zap.Error(err))
	}

	for {
		select {
		case msg := <-brokerStream:
			if msg != nil {
				if h.metrics != nil && h.metrics.HotPathEnabled {
					now := time.Now()
					if !msg.InternalCreatedAt.IsZero() {
						delay := now.Sub(msg.InternalCreatedAt)
						if delay < 0 {
							delay = 0
						}
						h.metrics.BrokerToHubDelay.Observe(delay.Seconds())
					}
					msg.BrokerReceivedAt = now
				}
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
			_ = h.broker.Close()

			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
			h.clientsMu.Lock()
			for c := range h.clients {
				_ = c.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(2*time.Second))
				_ = c.conn.Close()
			}
			h.clientsMu.Unlock()

			h.wg.Wait()
			h.logger.Info("Hub: shutdown complete")
			return
		}
	}
}
