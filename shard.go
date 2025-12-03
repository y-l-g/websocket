package websocket

import (
	"context"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type HubShard struct {
	id          int
	subs        *SubscriptionManager
	broadcast   chan *BroadcastMessage
	subscribe   chan *Subscription
	unsubscribe chan *Subscription
	clientMsg   chan *ClientMessageWrapper

	// Connection Management
	register chan *Client
	remove   chan *Client // acts as unregister
	clients  map[*Client]bool

	logger *zap.Logger
	ctx    context.Context
}

func NewHubShard(id int, logger *zap.Logger, ctx context.Context, metrics *Metrics, webhook *WebhookManager) *HubShard {
	return &HubShard{
		id:          id,
		subs:        NewSubscriptionManager(logger, metrics, webhook),
		broadcast:   make(chan *BroadcastMessage),
		subscribe:   make(chan *Subscription),
		unsubscribe: make(chan *Subscription),
		clientMsg:   make(chan *ClientMessageWrapper),

		register: make(chan *Client),
		remove:   make(chan *Client),
		clients:  make(map[*Client]bool),

		logger: logger,
		ctx:    ctx,
	}
}

func (s *HubShard) Run() {
	for {
		select {
		case c := <-s.register:
			s.clients[c] = true

		case c := <-s.remove:
			if _, ok := s.clients[c]; ok {
				delete(s.clients, c)
				s.subs.RemoveClient(c)
			}

		case sub := <-s.subscribe:
			s.subs.Subscribe(sub.Client, sub.Channel, sub.AuthData)

		case sub := <-s.unsubscribe:
			s.subs.Unsubscribe(sub.Client, sub.Channel)

		case msg := <-s.broadcast:
			s.subs.BroadcastToChannel(msg)

		case cMsg := <-s.clientMsg:
			s.subs.BroadcastToOthers(cMsg.Client, cMsg.Channel, cMsg.Event, cMsg.Data)

		case <-s.ctx.Done():
			// Graceful Shutdown: Close all connections in this shard
			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "Server Shutdown")
			for c := range s.clients {
				c.conn.WriteMessage(websocket.CloseMessage, closeMsg)
				c.conn.Close()
			}
			return
		}
	}
}
