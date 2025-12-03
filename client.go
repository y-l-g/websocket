package websocket

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 // 512 bytes limit for control messages (subscribe/ping)
)

type Client struct {
	ID      string
	hub     *Hub
	conn    *websocket.Conn
	send    chan []byte
	Headers http.Header
}

type ClientMessage struct {
	Event   string          `json:"event"`
	Channel string          `json:"channel,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type SubscribeData struct {
	Channel string `json:"channel"`
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	// We allow slightly larger messages for Client Events (Whispers),
	// but control messages should be small.
	// gorilla/websocket ReadLimit applies to the frame.
	c.conn.SetReadLimit(MaxDataSize + 1024)

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

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

	// 1. Validate Event Length
	if len(msg.Event) > MaxEventLength {
		return
	}

	// 2. Handle Client Events (Whispers)
	if strings.HasPrefix(msg.Event, "client-") {
		if msg.Channel == "" || len(msg.Channel) > MaxChannelLength {
			return
		}
		if len(msg.Data) > MaxDataSize {
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

	// 3. Handle Protocol Events
	switch msg.Event {
	case "pusher:ping":
		c.send <- []byte(`{"event":"pusher:pong"}`)

	case "pusher:subscribe":
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}

		if len(subData.Channel) > MaxChannelLength {
			return
		}

		var authData json.RawMessage

		if strings.HasPrefix(subData.Channel, "private-") || strings.HasPrefix(subData.Channel, "presence-") {
			result := c.hub.Authorize(c, subData.Channel)
			if !result.Allowed {
				errMsg, _ := json.Marshal(map[string]interface{}{
					"event": "pusher:error",
					"data": map[string]interface{}{
						"code":    4009,
						"message": "Subscription to " + subData.Channel + " rejected",
					},
				})
				c.send <- errMsg
				return
			}
			authData = result.UserData
		}

		c.hub.subscribe <- &Subscription{
			Client:   c,
			Channel:  subData.Channel,
			AuthData: authData,
		}

	case "pusher:unsubscribe":
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}
		// No need to validate length here, if it's too long it won't match anyway
		c.hub.unsubscribe <- &Subscription{Client: c, Channel: subData.Channel}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
