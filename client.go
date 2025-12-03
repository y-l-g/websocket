package websocket

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pogo/websocket/internal/protocol"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
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
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(protocol.MaxDataSize + 1024)
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

	if len(msg.Event) > protocol.MaxEventLength {
		return
	}

	// Whispers (Client Events)
	if strings.HasPrefix(msg.Event, "client-") {
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
		// Respond with Pong
		// We manually construct the JSON to avoid overhead
		c.send <- []byte(`{"event":"` + protocol.EventPong + `"}`)

	case protocol.EventSubscribe:
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}

		if len(subData.Channel) > protocol.MaxChannelLength {
			return
		}

		var authData json.RawMessage

		if strings.HasPrefix(subData.Channel, "private-") || strings.HasPrefix(subData.Channel, "presence-") {
			result := c.hub.Authorize(c, subData.Channel)
			if !result.Allowed {
				errMsg, _ := json.Marshal(map[string]interface{}{
					"event": protocol.EventError,
					"data": map[string]interface{}{
						"code":    protocol.ErrorSubscriptionDenied,
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

	case protocol.EventUnsubscribe:
		var subData SubscribeData
		if err := json.Unmarshal(msg.Data, &subData); err != nil {
			return
		}
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
