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
	"golang.org/x/time/rate"
)

const (
	DefaultWriteWait      = 10 * time.Second
	DefaultPongWait       = 60 * time.Second
	DefaultPingPeriod     = (DefaultPongWait * 9) / 10
	maxMessageSize        = 512
	DefaultWriteBurstSize = 64
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

	PingPeriod     time.Duration
	WriteWait      time.Duration
	PongWait       time.Duration
	WriteBurstSize int
	msgLimiter     *rate.Limiter
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
	Channel     string `json:"channel"`
	Auth        string `json:"auth,omitempty"`
	ChannelData string `json:"channel_data,omitempty"`
}

type SignInData struct {
	Auth     string `json:"auth"`
	UserData string `json:"user_data"`
}

type queuedOutboundMessage struct {
	payload    any
	enqueuedAt time.Time
}

func (c *Client) Send(msg any) {
	depth := len(c.send)
	if c.hub != nil && c.hub.metrics != nil && c.hub.metrics.HotPathEnabled {
		c.hub.metrics.ClientQueueDepth.Observe(float64(depth))
		msg = queuedOutboundMessage{
			payload:    msg,
			enqueuedAt: time.Now(),
		}
	}

	select {
	case c.send <- msg:
	default:
		if c.hub != nil && c.hub.metrics != nil {
			c.hub.metrics.DroppedMessages.WithLabelValues(c.hub.AppID, "queue_full", outboundMetricKind(msg)).Inc()
		}
	}
}

func outboundMetricKind(msg any) string {
	if queued, ok := msg.(queuedOutboundMessage); ok {
		msg = queued.payload
	}
	switch msg.(type) {
	case *websocket.PreparedMessage:
		return "prepared"
	case []byte:
		return "bytes"
	default:
		return "unknown"
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
	if c.msgLimiter != nil && !c.msgLimiter.Allow() {
		c.hub.logger.Warn("Client message rate limit exceeded", zap.String("id", c.ID))
		errMsg, _ := json.Marshal(map[string]interface{}{
			"event": protocol.EventError,
			"data": map[string]interface{}{
				"code":    protocol.ErrorGenericReconnect,
				"message": "Message rate limit exceeded",
			},
		})
		c.Send(errMsg)
		return
	}

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

		if !c.hub.EnqueueClientMessage(&ClientMessageWrapper{
			Client:  c,
			Channel: msg.Channel,
			Event:   msg.Event,
			Data:    msg.Data,
		}) {
			c.SendError(protocol.ErrorGenericReconnect, "Server overloaded, retry later")
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
			result := c.hub.Authorize(c, subData.Channel, subData.Auth, subData.ChannelData)
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

		if !c.hub.EnqueueSubscribe(&Subscription{
			Client:   c,
			Channel:  subData.Channel,
			AuthData: authData,
		}) {
			c.SendError(protocol.ErrorGenericReconnect, "Server overloaded, retry later")
		}

	case protocol.EventUnsubscribe:
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}
		if !protocol.IsValidChannelName(subData.Channel) {
			return
		}
		if !c.hub.EnqueueUnsubscribe(&Subscription{Client: c, Channel: subData.Channel}) {
			c.SendError(protocol.ErrorGenericReconnect, "Server overloaded, retry later")
		}

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

func (c *Client) SendError(code int, message string) {
	errMsg, _ := json.Marshal(map[string]interface{}{
		"event": protocol.EventError,
		"data": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
	c.Send(errMsg)
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
			if err := c.writeWithDeadline("close", func() error {
				return c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			}); err != nil {
				return
			}
			return

		case message, ok := <-c.send:
			if !ok {
				if err := c.writeWithDeadline("close", func() error {
					return c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				}); err != nil {
					return
				}
				return
			}

			if err := c.writeOutboundBurst(message); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.writeWithDeadline("ping", func() error {
				return c.conn.WriteMessage(websocket.PingMessage, nil)
			}); err != nil {
				return
			}
		}
	}
}

func (c *Client) writeOutboundBurst(first any) error {
	deadlineStart := time.Now()
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))

	if err := c.writeQueuedOutbound(first, deadlineStart, true); err != nil {
		return err
	}

	burstSize := c.WriteBurstSize
	if burstSize <= 0 {
		burstSize = DefaultWriteBurstSize
	}
	for i := 1; i < burstSize; i++ {
		select {
		case message, ok := <-c.send:
			if !ok {
				return c.writeWithDeadline("close", func() error {
					return c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				})
			}
			if err := c.writeQueuedOutbound(message, time.Now(), false); err != nil {
				return err
			}
		default:
			return nil
		}
	}

	return nil
}

func (c *Client) writeQueuedOutbound(message any, start time.Time, includesDeadline bool) error {
	payload := message
	if queued, ok := message.(queuedOutboundMessage); ok {
		payload = queued.payload
		if c.hub != nil && c.hub.metrics != nil && c.hub.metrics.HotPathEnabled {
			c.hub.metrics.ClientQueueResidence.Observe(time.Since(queued.enqueuedAt).Seconds())
		}
	}

	switch v := payload.(type) {
	case []byte:
		return c.writeOutboundMessage("bytes", start, includesDeadline, func() error {
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return err
			}
			if _, err := w.Write(v); err != nil {
				return err
			}
			return w.Close()
		})
	case *websocket.PreparedMessage:
		return c.writeOutboundMessage("prepared", start, includesDeadline, func() error {
			return c.conn.WritePreparedMessage(v)
		})
	default:
		return nil
	}
}

func (c *Client) writeOutboundMessage(kind string, start time.Time, includesDeadline bool, fn func() error) error {
	err := c.writeMessage(kind, fn)

	if c.hub != nil && c.hub.metrics != nil && c.hub.metrics.HotPathEnabled {
		metricKind := kind
		if includesDeadline {
			metricKind += "_with_deadline"
		}
		c.hub.metrics.WriteTotalDuration.WithLabelValues(metricKind).Observe(time.Since(start).Seconds())
	}

	return err
}

func (c *Client) writeWithDeadline(kind string, fn func() error) error {
	start := time.Now()
	_ = c.conn.SetWriteDeadline(time.Now().Add(c.WriteWait))
	err := c.writeMessage(kind, fn)

	if c.hub != nil && c.hub.metrics != nil && c.hub.metrics.HotPathEnabled {
		c.hub.metrics.WriteTotalDuration.WithLabelValues(kind).Observe(time.Since(start).Seconds())
	}

	return err
}

func (c *Client) writeMessage(kind string, fn func() error) error {
	start := time.Now()
	err := fn()

	if c.hub != nil && c.hub.metrics != nil {
		if c.hub.metrics.HotPathEnabled {
			c.hub.metrics.WriteDuration.WithLabelValues(kind).Observe(time.Since(start).Seconds())
		}
		if err != nil {
			c.hub.metrics.WriteFailures.WithLabelValues(kind).Inc()
		}
	}

	return err
}
