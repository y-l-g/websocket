package websocket

import (
	"context"

	"go.uber.org/zap"
)

type HubShard struct {
	id          int
	subs        *SubscriptionManager
	broadcast   chan *BroadcastMessage
	subscribe   chan *Subscription
	unsubscribe chan *Subscription
	clientMsg   chan *ClientMessageWrapper
	cleanup     chan *Client
	logger      *zap.Logger
	ctx         context.Context
}

func NewHubShard(id int, logger *zap.Logger, ctx context.Context, metrics *Metrics, webhook *WebhookManager) *HubShard {
	return &HubShard{
		id:          id,
		subs:        NewSubscriptionManager(logger, metrics, webhook),
		broadcast:   make(chan *BroadcastMessage),
		subscribe:   make(chan *Subscription),
		unsubscribe: make(chan *Subscription),
		clientMsg:   make(chan *ClientMessageWrapper),
		cleanup:     make(chan *Client),
		logger:      logger,
		ctx:         ctx,
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
			s.subs.BroadcastToChannel(msg)

		case cMsg := <-s.clientMsg:
			s.subs.BroadcastToOthers(cMsg.Client, cMsg.Channel, cMsg.Event, cMsg.Data)

		case <-s.ctx.Done():
			return
		}
	}
}
