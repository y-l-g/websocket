package websocket

import (
	"context"
	"encoding/json"

	"github.com/gorilla/websocket"
	"github.com/y-l-g/websocket/internal/protocol"
	"go.uber.org/zap"
)

type HubShard struct {
	id          int
	subs        *SubscriptionManager
	broadcast   chan *BroadcastMessage
	subscribe   chan *Subscription
	unsubscribe chan *Subscription
	clientMsg   chan *ClientMessageWrapper
	register    chan *Client
	remove      chan *Client
	clients     map[*Client]bool
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
		register:    make(chan *Client),
		remove:      make(chan *Client),
		clients:     make(map[*Client]bool),
		logger:      logger,
		ctx:         ctx,
	}
}

func (s *HubShard) Run() {
	for {
		select {
		case c := <-s.register:
			// Gestion de la connexion TCP (Local au Shard)
			s.clients[c] = true

			// Envoi du Handshake Pusher
			dataMap := map[string]interface{}{
				"socket_id":        c.ID,
				"activity_timeout": 120,
			}
			dataBytes, _ := json.Marshal(dataMap)
			payload := map[string]interface{}{
				"event": protocol.EventConnectionEstablished,
				"data":  string(dataBytes),
			}
			msg, _ := json.Marshal(payload)

			select {
			case c.send <- msg:
			default:
				delete(s.clients, c)
				c.conn.Close()
			}

		case c := <-s.remove:
			// CORRECTION FANTÔMES :
			// On nettoie TOUJOURS dans le SubscriptionManager, même si on ne gère pas la connexion TCP.
			s.subs.RemoveClient(c)

			// Si on gère la connexion TCP, on la supprime
			if _, ok := s.clients[c]; ok {
				delete(s.clients, c)
			}

		case sub := <-s.subscribe:
			// CORRECTION BLOCAGE : On accepte l'abonnement d'où qu'il vienne
			s.subs.Subscribe(sub.Client, sub.Channel, sub.AuthData)

		case sub := <-s.unsubscribe:
			s.subs.Unsubscribe(sub.Client, sub.Channel)

		case msg := <-s.broadcast:
			s.subs.BroadcastToChannel(msg)

		case cMsg := <-s.clientMsg:
			s.subs.BroadcastToOthers(cMsg.Client, cMsg.Channel, cMsg.Event, cMsg.Data)

		case <-s.ctx.Done():
			closeMsg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "Server Shutdown")
			for c := range s.clients {
				c.conn.WriteMessage(websocket.CloseMessage, closeMsg)
				c.conn.Close()
			}
			return
		}
	}
}
