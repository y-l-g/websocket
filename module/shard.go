package websocket

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type HubShard struct {
	id          int
	appID       string
	subs        *SubscriptionManager
	broadcast   chan *BroadcastMessage
	subscribe   chan *Subscription
	unsubscribe chan *Subscription
	clientMsg   chan *ClientMessageWrapper
	cleanup     chan *Client
	logger      *zap.Logger
	metrics     *Metrics
	ctx         context.Context
}

func NewHubShard(id int, appID string, logger *zap.Logger, ctx context.Context, metrics *Metrics, webhook *WebhookManager, queueSize int) *HubShard {
	if queueSize <= 0 {
		queueSize = DefaultShardQueueSize
	}
	return &HubShard{
		id:          id,
		appID:       appID,
		subs:        NewSubscriptionManager(logger, metrics, webhook),
		broadcast:   make(chan *BroadcastMessage, queueSize),
		subscribe:   make(chan *Subscription, queueSize),
		unsubscribe: make(chan *Subscription, queueSize),
		clientMsg:   make(chan *ClientMessageWrapper, queueSize),
		cleanup:     make(chan *Client, queueSize),
		logger:      logger,
		metrics:     metrics,
		ctx:         ctx,
	}
}

func (s *HubShard) enqueueBroadcast(msg *BroadcastMessage) {
	select {
	case s.broadcast <- msg:
	default:
		if s.metrics != nil {
			s.metrics.BrokerDropped.WithLabelValues(s.appID, "shard_queue_full").Inc()
		}
		trySendPublishResult(msg, PublishShardQueueFull)
	}
}

func (s *HubShard) EnqueueSubscribe(sub *Subscription) bool {
	select {
	case s.subscribe <- sub:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *HubShard) EnqueueUnsubscribe(sub *Subscription) bool {
	select {
	case s.unsubscribe <- sub:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *HubShard) EnqueueClientMessage(msg *ClientMessageWrapper) bool {
	select {
	case s.clientMsg <- msg:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *HubShard) Run() {
	for {
		select {
		case c := <-s.cleanup:
			s.subs.RemoveClient(c)

		case sub := <-s.subscribe:
			sub.Client.AddShard(s.id)
			s.subs.Subscribe(sub.Client, sub.Channel, sub.AuthData)

		case sub := <-s.unsubscribe:
			s.subs.Unsubscribe(sub.Client, sub.Channel)

		case msg := <-s.broadcast:
			trySendPublishResult(msg, PublishOK)
			if s.metrics != nil && s.metrics.HotPathEnabled && !msg.BrokerReceivedAt.IsZero() {
				now := time.Now()
				delay := now.Sub(msg.BrokerReceivedAt)
				if delay < 0 {
					delay = 0
				}
				s.metrics.HubToShardDelay.Observe(delay.Seconds())
				msg.ShardBroadcastAt = now
			}
			s.subs.BroadcastToChannel(msg)

		case cMsg := <-s.clientMsg:
			s.subs.BroadcastToOthers(cMsg.Client, cMsg.Channel, cMsg.Event, cMsg.Data)

		case <-s.ctx.Done():
			return
		}
	}
}

func trySendPublishResult(msg *BroadcastMessage, status PublishStatus) {
	if msg == nil || msg.LocalResult == nil {
		return
	}
	select {
	case msg.LocalResult <- status:
	default:
	}
}
