package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/y-l-g/websocket/internal/protocol"
	"go.uber.org/zap"
)

// Defaults used if config is missing
const (
	DefaultWriteWait  = 10 * time.Second
	DefaultPongWait   = 60 * time.Second
	DefaultPingPeriod = (DefaultPongWait * 9) / 10
	maxMessageSize    = 512
)

type Client struct {
	ID      string
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	Headers http.Header
	ctx     context.Context
	cancel  context.CancelFunc

	shardMask uint32

	// Configurable Timeouts
	PingPeriod time.Duration
	WriteWait  time.Duration
	PongWait   time.Duration
}

// AddShard marks that the client has a subscription on the given shard ID.
func (c *Client) AddShard(id int) {
	if id < 0 || id >= 32 {
		return
	}
	atomic.OrUint32(&c.shardMask, uint32(1)<<id)
}

// HasShard checks if the client might have resources on the given shard ID.
func (c *Client) HasShard(id int) bool {
	if id < 0 || id >= 32 {
		return false
	}
	mask := atomic.LoadUint32(&c.shardMask)
	return (mask & (uint32(1) << id)) != 0
}

type ClientMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type SubscribeData struct {
	Channel string `json:"channel"`
}

func (c *Client) Send(msg []byte) {
	select {
	case c.send <- msg:
	default:
		if c.hub != nil && c.hub.metrics != nil {
			c.hub.metrics.DroppedMessages.Inc()
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		_ = c.conn.Close() // Explicitly ignore close error on defer
		c.cancel()
	}()

	c.conn.SetReadLimit(protocol.MaxDataSize + 1024)

	// Explicitly ignore deadline errors in setup/pong handler
	_ = c.conn.SetReadDeadline(time.Now().Add(c.PongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Error("Websocket read error", zap.Error(err))
			}
			break
		}
		c.handleMessage(message)
	}
}

func (c *Client) handleMessage(message []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		c.hub.logger.Warn("Invalid JSON", zap.String("id", c.ID))
		return
	}

	if len(msg.Event) > protocol.MaxEventLength {
		return
	}

	if strings.HasPrefix(msg.Event, protocol.ChannelPrefixClient) {
		if msg.Channel == "" || len(msg.Channel) > protocol.MaxChannelLength {
			return
		}
		if len(msg.Data) > protocol.MaxDataSize {
			return
		}

		c.hub.clientMessage <- &ClientMessageWrapper{
			Client:  c,
			Channel: msg.Channel,
			Event:   msg.Event,
			Data:    msg.Data,
		}
		return
	}

	switch msg.Event {
	case protocol.EventPing:
		c.Send([]byte(`{"event":"` + protocol.EventPong + `"}`))

	case protocol.EventSubscribe:
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}

		if len(subData.Channel) > protocol.MaxChannelLength {
			return
		}

		var authData json.RawMessage

		if strings.HasPrefix(subData.Channel, protocol.ChannelPrefixPrivate) || strings.HasPrefix(subData.Channel, protocol.ChannelPrefixPresence) {
			result := c.hub.Authorize(c, subData.Channel)
			if !result.Allowed {
				errMsg, _ := json.Marshal(map[string]interface{}{
					"event": protocol.EventError,
					"data": map[string]interface{}{
						"code":    protocol.ErrorSubscriptionDenied,
						"message": "Subscription to " + subData.Channel + " rejected",
					},
				})
				c.Send(errMsg)
				return
			}
			authData = result.UserData
		}

		c.hub.subscribe <- &Subscription{
			Client:   c,
			Channel:  subData.Channel,
			AuthData: authData,
		}

	case protocol.EventUnsubscribe:
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}
		c.hub.unsubscribe <- &Subscription{Client: c, Channel: subData.Channel}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(c.PingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close() // Explicitly ignore error
	}()

	for {
		select {
		case <-c.ctx.Done():
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return

		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			// Check write error
			if _, err := w.Write(message); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
