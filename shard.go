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
	remove      chan *Client // For cleaning up disconnected clients
	logger      *zap.Logger
	ctx         context.Context
}

func NewHubShard(id int, logger *zap.Logger, ctx context.Context, webhook *WebhookManager) *HubShard {
	return &HubShard{
		id:          id,
		subs:        NewSubscriptionManager(logger, webhook),
		broadcast:   make(chan *BroadcastMessage),
		subscribe:   make(chan *Subscription),
		unsubscribe: make(chan *Subscription),
		clientMsg:   make(chan *ClientMessageWrapper),
		remove:      make(chan *Client),
		logger:      logger,
		ctx:         ctx,
	}
}

func (s *HubShard) Run() {
	for {
		select {
		case sub := <-s.subscribe:
			s.subs.Subscribe(sub.Client, sub.Channel, sub.AuthData)

		case sub := <-s.unsubscribe:
			s.subs.Unsubscribe(sub.Client, sub.Channel)

		case msg := <-s.broadcast:
			s.subs.BroadcastToChannel(msg)

		case cMsg := <-s.clientMsg:
			s.subs.BroadcastToOthers(cMsg.Client, cMsg.Channel, cMsg.Event, cMsg.Data)

		case client := <-s.remove:
			// Client disconnected, cleanup ANY subscriptions they had on this shard
			s.subs.RemoveClient(client)

		case <-s.ctx.Done():
			return
		}
	}
}
