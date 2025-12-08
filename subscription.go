package websocket

import (
	"encoding/json"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/y-l-g/websocket/internal/protocol"
	"go.uber.org/zap"
)

type Member struct {
	UserID   string          `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info"`
}

type SubscriptionManager struct {
	channels     map[string]map[*Client]bool
	clients      map[*Client]map[string]bool
	presence     map[string]map[string]Member
	clientToUser map[string]map[*Client]string
	logger       *zap.Logger
	webhook      *WebhookManager
	metrics      *Metrics
}

func NewSubscriptionManager(logger *zap.Logger, metrics *Metrics, webhook *WebhookManager) *SubscriptionManager {
	return &SubscriptionManager{
		channels:     make(map[string]map[*Client]bool),
		clients:      make(map[*Client]map[string]bool),
		presence:     make(map[string]map[string]Member),
		clientToUser: make(map[string]map[*Client]string),
		logger:       logger,
		webhook:      webhook,
		metrics:      metrics,
	}
}

func (sm *SubscriptionManager) BroadcastToChannel(msg *BroadcastMessage) {
	clients := sm.GetClients(msg.Channel)
	if len(clients) == 0 {
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"event":   msg.Event,
		"channel": msg.Channel,
		"data":    string(msg.Data),
	})
	if err != nil {
		sm.logger.Error("JSON marshal error", zap.Error(err))
		return
	}

	pm, err := websocket.NewPreparedMessage(websocket.TextMessage, payload)
	if err != nil {
		sm.logger.Error("PreparedMessage error", zap.Error(err))
		for client := range clients {
			client.Send(payload)
		}
		return
	}

	for client := range clients {
		client.Send(pm)
	}
}

func (sm *SubscriptionManager) Subscribe(client *Client, channel string, userData json.RawMessage) {
	isNewChannel := false
	if _, ok := sm.channels[channel]; !ok {
		sm.channels[channel] = make(map[*Client]bool)
		isNewChannel = true
	}
	sm.channels[channel][client] = true

	if _, ok := sm.clients[client]; !ok {
		sm.clients[client] = make(map[string]bool)
	}
	sm.clients[client][channel] = true

	sm.metrics.Subscriptions.Inc()

	if isNewChannel && sm.webhook != nil {
		sm.webhook.Notify("channel_occupied", channel)
	}

	if strings.HasPrefix(channel, protocol.ChannelPrefixPresence) {
		sm.handlePresenceSubscribe(client, channel, userData)
	} else {
		msg, _ := json.Marshal(map[string]interface{}{
			"event":   protocol.EventSubscriptionSucceeded,
			"channel": channel,
			"data":    "{}",
		})
		client.Send(msg)
	}
}

func (sm *SubscriptionManager) BroadcastToOthers(sender *Client, channel, event string, data json.RawMessage) {
	if !strings.HasPrefix(channel, protocol.ChannelPrefixPrivate) && !strings.HasPrefix(channel, protocol.ChannelPrefixPresence) {
		return
	}
	if chans, ok := sm.clients[sender]; !ok || !chans[channel] {
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"event":   event,
		"channel": channel,
		"data":    string(data),
	})
	if err != nil {
		return
	}

	pm, err := websocket.NewPreparedMessage(websocket.TextMessage, payload)
	if err != nil {
		return
	}

	clients := sm.channels[channel]
	for client := range clients {
		if client == sender {
			continue
		}
		client.Send(pm)
	}
}

type PresenceAuthResponse struct {
	Auth        string `json:"auth"`
	ChannelData string `json:"channel_data"`
}

type PresenceChannelData struct {
	UserID   json.RawMessage `json:"user_id"`
	UserInfo json.RawMessage `json:"user_info"`
}

func (sm *SubscriptionManager) handlePresenceSubscribe(client *Client, channel string, userData json.RawMessage) {
	var authResp PresenceAuthResponse
	if err := json.Unmarshal(userData, &authResp); err != nil {
		sm.logger.Warn("Presence: invalid auth response", zap.String("id", client.ID))
		return
	}

	if authResp.ChannelData == "" {
		sm.logger.Warn("Presence: missing channel_data", zap.String("id", client.ID))
		return
	}

	var chanData PresenceChannelData
	if err := json.Unmarshal([]byte(authResp.ChannelData), &chanData); err != nil {
		sm.logger.Warn("Presence: invalid channel_data JSON", zap.String("id", client.ID))
		return
	}

	userID := strings.Trim(string(chanData.UserID), "\"")
	if userID == "" || userID == "null" {
		sm.logger.Warn("Presence: missing user_id", zap.String("id", client.ID))
		return
	}

	userInfo := chanData.UserInfo
	if len(userInfo) == 0 {
		userInfo = json.RawMessage("null")
	}

	member := Member{UserID: userID, UserInfo: userInfo}

	if _, ok := sm.presence[channel]; !ok {
		sm.presence[channel] = make(map[string]Member)
		sm.clientToUser[channel] = make(map[*Client]string)
	}

	_, alreadyPresent := sm.presence[channel][userID]
	sm.presence[channel][userID] = member
	sm.clientToUser[channel][client] = userID

	ids := []string{}
	hash := make(map[string]interface{})
	for uid, m := range sm.presence[channel] {
		ids = append(ids, uid)
		var info interface{}
		_ = json.Unmarshal(m.UserInfo, &info)
		hash[uid] = info
	}

	presencePayload := map[string]interface{}{
		"presence": map[string]interface{}{
			"ids":  ids,
			"hash": hash,
		},
	}
	dataJson, _ := json.Marshal(presencePayload)
	successMsg, _ := json.Marshal(map[string]interface{}{
		"event":   protocol.EventSubscriptionSucceeded,
		"channel": channel,
		"data":    string(dataJson),
	})
	client.Send(successMsg)

	if !alreadyPresent {
		addedData := map[string]interface{}{
			"user_id":   userID,
			"user_info": member.UserInfo,
		}
		addedDataBytes, _ := json.Marshal(addedData)
		addedMsg, _ := json.Marshal(map[string]interface{}{
			"event":   protocol.EventMemberAdded,
			"channel": channel,
			"data":    string(addedDataBytes),
		})

		pm, _ := websocket.NewPreparedMessage(websocket.TextMessage, addedMsg)

		for otherClient := range sm.channels[channel] {
			if otherClient != client {
				if pm != nil {
					otherClient.Send(pm)
				} else {
					otherClient.Send(addedMsg)
				}
			}
		}
	}
}

func (sm *SubscriptionManager) Unsubscribe(client *Client, channel string) {
	if clients, ok := sm.channels[channel]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(sm.channels, channel)
			if sm.webhook != nil {
				sm.webhook.Notify("channel_vacated", channel)
			}
		}
	}
	if chans, ok := sm.clients[client]; ok {
		delete(chans, channel)
	}
	if strings.HasPrefix(channel, protocol.ChannelPrefixPresence) {
		sm.handlePresenceUnsubscribe(client, channel)
	}
}

func (sm *SubscriptionManager) handlePresenceUnsubscribe(client *Client, channel string) {
	if clientMap, ok := sm.clientToUser[channel]; ok {
		if userID, ok := clientMap[client]; ok {
			delete(clientMap, client)
			stillPresent := false
			for _, uid := range clientMap {
				if uid == userID {
					stillPresent = true
					break
				}
			}
			if !stillPresent {
				delete(sm.presence[channel], userID)
				removedData := map[string]interface{}{
					"user_id": userID,
				}
				removedDataBytes, _ := json.Marshal(removedData)
				removedMsg, _ := json.Marshal(map[string]interface{}{
					"event":   protocol.EventMemberRemoved,
					"channel": channel,
					"data":    string(removedDataBytes),
				})

				pm, _ := websocket.NewPreparedMessage(websocket.TextMessage, removedMsg)

				if clients, ok := sm.channels[channel]; ok {
					for sub := range clients {
						if pm != nil {
							sub.Send(pm)
						} else {
							sub.Send(removedMsg)
						}
					}
				}
			}
		}
	}
}

func (sm *SubscriptionManager) RemoveClient(client *Client) {
	if chans, ok := sm.clients[client]; ok {
		for channel := range chans {
			sm.Unsubscribe(client, channel)
		}
		delete(sm.clients, client)
	}
}

func (sm *SubscriptionManager) GetClients(channel string) map[*Client]bool {
	return sm.channels[channel]
}
