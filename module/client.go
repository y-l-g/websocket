package websocket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/y-l-g/websocket/module/internal/protocol"
	"go.uber.org/zap"
)

const (
	DefaultWriteWait  = 10 * time.Second
	DefaultPongWait   = 60 * time.Second
	DefaultPingPeriod = (DefaultPongWait * 9) / 10
	maxMessageSize    = 512
)

type WSConnection interface {
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	WritePreparedMessage(pm *websocket.PreparedMessage) error
	NextWriter(messageType int) (io.WriteCloser, error)
	Close() error
	SetWriteDeadline(t time.Time) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
}

type Client struct {
	ID      string
	hub     *Hub
	conn    WSConnection
	send    chan any
	Headers http.Header
	ctx     context.Context
	cancel  context.CancelFunc

	shardMask uint64

	PingPeriod time.Duration
	WriteWait  time.Duration
	PongWait   time.Duration
}

// AddShard marks that the client has a subscription on the given shard ID.
func (c *Client) AddShard(id int) {
	if id < 0 || id >= 64 {
		return
	}
	atomic.OrUint64(&c.shardMask, uint64(1)<<id)
}

// HasShard checks if the client might have resources on the given shard ID.
func (c *Client) HasShard(id int) bool {
	if id < 0 || id >= 64 {
		return false
	}
	mask := atomic.LoadUint64(&c.shardMask)
	return (mask & (uint64(1) << id)) != 0
}

type ClientMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type SubscribeData struct {
	Channel string `json:"channel"`
}

type SignInData struct {
	Auth     string `json:"auth"`
	UserData string `json:"user_data"`
}

func (c *Client) Send(msg any) {
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
		_ = c.conn.Close()
		c.cancel()
	}()

	c.conn.SetReadLimit(protocol.MaxDataSize + 1024)

	_ = c.conn.SetReadDeadline(time.Now().Add(c.PongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.PongWait))
		return nil
	})

	for {
		msgType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Error("Websocket read error", zap.Error(err))
			}
			break
		}

		if msgType == websocket.BinaryMessage {
			c.hub.logger.Warn("Client sent binary frame, disconnecting", zap.String("id", c.ID))
			// Send 4003 Error/Close
			deadline := time.Now().Add(time.Second)
			msg := websocket.FormatCloseMessage(protocol.ErrorApplicationDisabled, "Binary frames not supported")
			_ = c.conn.WriteControl(websocket.CloseMessage, msg, deadline)
			return
		}

		c.handleMessage(message)
	}
}

func (c *Client) handleMessage(message []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		c.hub.logger.Warn("Invalid JSON", zap.String("id", c.ID))

		errMsg, _ := json.Marshal(map[string]interface{}{
			"event": protocol.EventError,
			"data": map[string]interface{}{
				"code":    protocol.ErrorGenericReconnect, // 4200 Generic
				"message": "Invalid JSON format",
			},
		})
		c.Send(errMsg)
		return
	}

	if len(msg.Event) > protocol.MaxEventLength {
		return
	}

	if strings.HasPrefix(msg.Event, protocol.ChannelPrefixClient) {
		if !protocol.IsValidChannelName(msg.Channel) {
			c.hub.logger.Warn("Invalid channel name in client event", zap.String("channel", msg.Channel))
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

		if !protocol.IsValidChannelName(subData.Channel) {
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
		if !protocol.IsValidChannelName(subData.Channel) {
			return
		}
		c.hub.unsubscribe <- &Subscription{Client: c, Channel: subData.Channel}

	case protocol.EventSignin:
		var signin SignInData
		if err := json.Unmarshal(msg.Data, &signin); err != nil {
			return // Malformed data
		}

		result := c.hub.auth.AuthenticateUser(c, signin.Auth, signin.UserData)
		if result.Allowed {
			// Success: pusher:signin_success with user_data
			respPayload, _ := json.Marshal(map[string]interface{}{
				"event": protocol.EventSigninSuccess,
				"data": map[string]interface{}{
					"user_data": string(result.UserData),
				},
			})
			c.Send(respPayload)
		} else {
			// Failure
			errPayload, _ := json.Marshal(map[string]interface{}{
				"event": protocol.EventError,
				"data": map[string]interface{}{
					"code":    protocol.ErrorSubscriptionDenied, // 4009
					"message": "Signin authentication failed",
				},
			})
			c.Send(errPayload)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(c.PingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
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

			switch v := message.(type) {
			case []byte:
				w, err := c.conn.NextWriter(websocket.TextMessage)
				if err != nil {
					return
				}
				if _, err := w.Write(v); err != nil {
					return
				}
				if err := w.Close(); err != nil {
					return
				}
			case *websocket.PreparedMessage:
				if err := c.conn.WritePreparedMessage(v); err != nil {
					return
				}
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
