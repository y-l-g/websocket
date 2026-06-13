package websocket

import (
	"context"
	"encoding/json"
	"errors"
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
	DefaultBrokerQueueSize    = 1024
	DefaultShardQueueSize     = 1024
	DefaultShutdownTimeout    = 10 * time.Second
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
	PublishBrokerQueueFull
	PublishShardQueueFull
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
	maxConnections  int64
	numShards       int
	activityTimeout int // Seconds
	shutdownTimeout time.Duration
	healthy         atomic.Bool
	healthErr       atomic.Value

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
	AppID             string             `json:"app_id"`
	Channel           string             `json:"channel"`
	Event             string             `json:"event"`
	Data              json.RawMessage    `json:"data"`
	ExceptSocketID    string             `json:"socket_id,omitempty"`
	InternalCreatedAt time.Time          `json:"-"`
	BrokerReceivedAt  time.Time          `json:"-"`
	ShardBroadcastAt  time.Time          `json:"-"`
	LocalResult       chan PublishStatus `json:"-"`
}

type PublishOptions struct {
	ExceptSocketID string
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
	BrokerQueueSize    int
	ShardQueueSize     int
	ShutdownTimeout    time.Duration
}

func DefaultDeliveryConfig() DeliveryConfig {
	return DeliveryConfig{
		OutboundQueueSize:  DefaultOutboundQueueSize,
		WriteBurstSize:     DefaultWriteBurstSize,
		ClientMsgRateLimit: DefaultClientMsgRateLimit,
		ClientMsgRateBurst: DefaultClientMsgRateBurst,
		EnableCompression:  false,
		BrokerQueueSize:    DefaultBrokerQueueSize,
		ShardQueueSize:     DefaultShardQueueSize,
		ShutdownTimeout:    DefaultShutdownTimeout,
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
	if c.BrokerQueueSize <= 0 {
		c.BrokerQueueSize = defaults.BrokerQueueSize
	}
	if c.ShardQueueSize <= 0 {
		c.ShardQueueSize = defaults.ShardQueueSize
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = defaults.ShutdownTimeout
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
		AppID:           appID,
		auth:            auth,
		broker:          broker,
		logger:          logger,
		metrics:         metrics,
		ctx:             ctx,
		maxConnections:  int64(maxConn),
		numShards:       numShards,
		activityTimeout: timeoutSec,
		shutdownTimeout: delivery.ShutdownTimeout,
		done:            make(chan struct{}),
		clientMessage:   make(chan *ClientMessageWrapper, delivery.ShardQueueSize),
		subscribe:       make(chan *Subscription, delivery.ShardQueueSize),
		unsubscribe:     make(chan *Subscription, delivery.ShardQueueSize),
		shards:          make([]*HubShard, numShards),
		clients:         make(map[*Client]bool),
	}

	for i := 0; i < numShards; i++ {
		h.shards[i] = NewHubShard(i, appID, logger, ctx, metrics, webhook, delivery.ShardQueueSize)
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

func (h *Hub) Register(c *Client) bool {
	h.clientsMu.Lock()
	if int64(len(h.clients)) >= h.maxConnections {
		h.clientsMu.Unlock()
		h.logger.Warn("Hub: max connections reached, rejecting", zap.String("id", c.ID))

		// Protocol Compliance: Send 4100 Over Capacity
		deadline := time.Now().Add(1 * time.Second)
		msg := websocket.FormatCloseMessage(protocol.ErrorOverCapacity, "Over capacity")
		// Best effort write control
		_ = c.conn.WriteControl(websocket.CloseMessage, msg, deadline)
		_ = c.conn.Close()
		return false
	}
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
	return true
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

func (h *Hub) ConnectionIDs() []string {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	ids := make([]string, 0, len(h.clients))
	for client := range h.clients {
		ids = append(ids, client.ID)
	}
	return ids
}

func (h *Hub) ChannelSnapshots(filterByPrefix string) []ChannelSnapshot {
	snapshots := []ChannelSnapshot{}
	for _, shard := range h.shards {
		snapshots = append(snapshots, shard.ChannelSnapshots(filterByPrefix)...)
	}
	return snapshots
}

func (h *Hub) ChannelSnapshot(channel string) ChannelSnapshot {
	return h.getShard(channel).ChannelSnapshot(channel)
}

func (h *Hub) TerminateUserConnections(userID string) int {
	clients := make(map[*Client]struct{})

	h.clientsMu.RLock()
	for client := range h.clients {
		if client.UserID() == userID {
			clients[client] = struct{}{}
		}
	}
	h.clientsMu.RUnlock()

	for _, shard := range h.shards {
		for _, client := range shard.ClientsForUser(userID) {
			clients[client] = struct{}{}
		}
	}

	for client := range clients {
		client.Disconnect()
		h.Unregister(client)
	}

	return len(clients)
}

func (h *Hub) Wait() {
	<-h.done
}

func (h *Hub) Authorize(client *Client, channel string, auth string, channelData string) AuthResult {
	if len(channel) > protocol.MaxChannelLength {
		return AuthResult{Allowed: false}
	}
	if h.auth == nil {
		return AuthResult{Allowed: false}
	}
	return h.auth.Authorize(client, channel, auth, channelData)
}

func (h *Hub) EnqueueSubscribe(sub *Subscription) bool {
	select {
	case h.subscribe <- sub:
		return true
	case <-h.ctx.Done():
		return false
	}
}

func (h *Hub) EnqueueUnsubscribe(sub *Subscription) bool {
	select {
	case h.unsubscribe <- sub:
		return true
	case <-h.ctx.Done():
		return false
	}
}

func (h *Hub) EnqueueClientMessage(msg *ClientMessageWrapper) bool {
	select {
	case h.clientMessage <- msg:
		return true
	case <-h.ctx.Done():
		return false
	}
}

func (h *Hub) Publish(channel, event, data string) bool {
	return h.publish(channel, event, data) == PublishOK
}

func (h *Hub) PublishWithOptions(channel, event, data string, options PublishOptions) PublishStatus {
	return h.publishWithOptions(channel, event, data, options)
}

func (h *Hub) publish(channel, event, data string) PublishStatus {
	return h.publishWithOptions(channel, event, data, PublishOptions{})
}

func (h *Hub) publishWithOptions(channel, event, data string, options PublishOptions) PublishStatus {
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
		AppID:             h.AppID,
		Channel:           channel,
		Event:             event,
		Data:              raw,
		ExceptSocketID:    options.ExceptSocketID,
		InternalCreatedAt: time.Now(),
	}
	if h.supportsLocalPublishAck() {
		msg.LocalResult = make(chan PublishStatus, 1)
	}

	brokerStart := time.Now()
	err := h.broker.Publish(h.ctx, msg)
	if hotPath {
		h.metrics.PublishDuration.WithLabelValues("broker").Observe(time.Since(brokerStart).Seconds())
	}
	if err != nil {
		if errors.Is(err, ErrBrokerQueueFull) {
			if h.metrics != nil {
				h.metrics.BrokerDropped.WithLabelValues(h.AppID, "queue_full").Inc()
				h.metrics.PublishFailures.WithLabelValues(h.AppID, "broker_queue_full").Inc()
			}
			return PublishBrokerQueueFull
		}
		h.logger.Error("Hub: broker publish failed", zap.Error(err))
		if h.metrics != nil {
			h.metrics.PublishFailures.WithLabelValues(h.AppID, "broker_failed").Inc()
		}
		return PublishBrokerFailed
	}
	if msg.LocalResult != nil {
		select {
		case status := <-msg.LocalResult:
			if status != PublishOK && h.metrics != nil {
				h.metrics.PublishFailures.WithLabelValues(h.AppID, publishStatusMetricReason(status)).Inc()
			}
			return status
		case <-h.ctx.Done():
			return PublishBrokerFailed
		}
	}
	return PublishOK
}

func (h *Hub) supportsLocalPublishAck() bool {
	acker, ok := h.broker.(interface{ SupportsLocalPublishAck() bool })
	return ok && acker.SupportsLocalPublishAck()
}

func publishStatusMetricReason(status PublishStatus) string {
	switch status {
	case PublishBrokerQueueFull:
		return "broker_queue_full"
	case PublishShardQueueFull:
		return "shard_queue_full"
	case PublishBrokerFailed:
		return "broker_failed"
	default:
		return "failed"
	}
}

func (h *Hub) publishScope() string {
	if scoped, ok := h.broker.(interface{ PublishScope() string }); ok {
		return scoped.PublishScope()
	}
	return fmt.Sprintf("hub:%p", h)
}

func publishToActiveHubs(hubs []*Hub, channel, event, data string) PublishStatus {
	return publishToActiveHubsWithOptions(hubs, channel, event, data, PublishOptions{})
}

func publishToActiveHubsWithOptions(hubs []*Hub, channel, event, data string, options PublishOptions) PublishStatus {
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

		if result := hub.publishWithOptions(channel, event, data, options); result != PublishOK && status == PublishOK {
			status = result
		}
	}
	return status
}

func (h *Hub) IsHealthy() bool {
	return h.healthy.Load()
}

func (h *Hub) HealthError() string {
	if value := h.healthErr.Load(); value != nil {
		if msg, ok := value.(string); ok {
			return msg
		}
	}
	return ""
}

func (h *Hub) setHealth(healthy bool, err string) {
	h.healthy.Store(healthy)
	h.healthErr.Store(err)
}

func (h *Hub) Run() {
	defer close(h.done)
	h.logger.Info("Hub: started", zap.String("app_id", h.AppID), zap.Int("shards", h.numShards))

	brokerStream, err := h.broker.Subscribe(h.ctx)
	if err != nil {
		h.setHealth(false, "broker_subscribe_failed")
		h.logger.Error("Hub: failed to subscribe to broker", zap.Error(err))
		return
	}
	h.setHealth(true, "")

	for {
		select {
		case msg, ok := <-brokerStream:
			if !ok {
				h.setHealth(false, "broker_stream_closed")
				h.logger.Warn("Hub: broker stream closed")
				return
			}
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
				shard.enqueueBroadcast(msg)
			}

		case sub := <-h.subscribe:
			if len(sub.Channel) <= protocol.MaxChannelLength {
				shard := h.getShard(sub.Channel)
				shard.EnqueueSubscribe(sub)
			}

		case sub := <-h.unsubscribe:
			if len(sub.Channel) <= protocol.MaxChannelLength {
				shard := h.getShard(sub.Channel)
				shard.EnqueueUnsubscribe(sub)
			}

		case cMsg := <-h.clientMessage:
			shard := h.getShard(cMsg.Channel)
			shard.EnqueueClientMessage(cMsg)

		case <-h.ctx.Done():
			h.setHealth(false, "shutting_down")
			h.logger.Info("Hub: shutting down, draining connections...")
			_ = h.broker.Close()

			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down")
			h.clientsMu.Lock()
			for c := range h.clients {
				_ = c.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(2*time.Second))
				_ = c.conn.Close()
			}
			h.clientsMu.Unlock()

			drained := make(chan struct{})
			go func() {
				h.wg.Wait()
				close(drained)
			}()

			select {
			case <-drained:
				h.logger.Info("Hub: shutdown complete")
			case <-time.After(h.shutdownTimeout):
				h.logger.Warn("Hub: shutdown timeout reached", zap.Int64("remaining_connections", h.conns.Load()), zap.Duration("timeout", h.shutdownTimeout))
			}
			return
		}
	}
}
