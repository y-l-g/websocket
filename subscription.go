package websocket

import (
	"encoding/json"
	"fmt"
	"strings"

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
}

func NewSubscriptionManager(logger *zap.Logger, webhook *WebhookManager) *SubscriptionManager {
	return &SubscriptionManager{
		channels:     make(map[string]map[*Client]bool),
		clients:      make(map[*Client]map[string]bool),
		presence:     make(map[string]map[string]Member),
		clientToUser: make(map[string]map[*Client]string),
		logger:       logger,
		webhook:      webhook,
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
		"data":    msg.Data,
	})
	if err != nil {
		sm.logger.Error("JSON marshal error", zap.Error(err))
		return
	}

	for client := range clients {
		select {
		case client.send <- payload:
		default:
		}
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

	metricSubscriptions.Inc() // Metric

	if isNewChannel && sm.webhook != nil {
		sm.webhook.Notify("channel_occupied", channel)
	}

	if strings.HasPrefix(channel, "presence-") {
		sm.handlePresenceSubscribe(client, channel, userData)
	} else {
		msg, _ := json.Marshal(map[string]interface{}{
			"event":   "pusher_internal:subscription_succeeded",
			"channel": channel,
			"data":    "{}",
		})
		client.send <- msg
	}
}

// ... (Rest of the file remains unchanged, BroadcastToOthers, handlePresenceSubscribe, etc.)
// Just keeping the file consistent. The only change was adding metricSubscriptions.Inc()
// I will not reprint the whole file to save space unless requested, assuming previous content is preserved.
// Below are the required parts to be fully functional.

func (sm *SubscriptionManager) BroadcastToOthers(sender *Client, channel, event string, data json.RawMessage) {
	if !strings.HasPrefix(channel, "private-") && !strings.HasPrefix(channel, "presence-") {
		return
	}
	if chans, ok := sm.clients[sender]; !ok || !chans[channel] {
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"event":   event,
		"channel": channel,
		"data":    data,
	})
	if err != nil {
		return
	}
	clients := sm.channels[channel]
	for client := range clients {
		if client == sender {
			continue
		}
		select {
		case client.send <- payload:
		default:
		}
	}
}

func (sm *SubscriptionManager) handlePresenceSubscribe(client *Client, channel string, userData json.RawMessage) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(userData, &rawMap); err != nil {
		return
	}

	var userID string
	var userInfo interface{}

	if v, ok := rawMap["user_id"]; ok {
		s := fmt.Sprintf("%v", v)
		if s != "" {
			userID = s
		}
	}
	if v, ok := rawMap["info"]; ok {
		userInfo = v
	} else if v, ok := rawMap["user_info"]; ok {
		userInfo = v
	} else {
		userInfo = rawMap
	}

	if userID == "" {
		var nested map[string]interface{}
		if v, ok := rawMap["user_info"].(map[string]interface{}); ok {
			nested = v
		} else if v, ok := rawMap["info"].(map[string]interface{}); ok {
			nested = v
		}
		if nested != nil {
			if v, ok := nested["user_id"]; ok {
				userID = fmt.Sprintf("%v", v)
			}
			if v, ok := nested["info"]; ok {
				userInfo = v
			} else if v, ok := nested["user_info"]; ok {
				userInfo = v
			}
		}
	}

	if userID == "" || userID == "<nil>" {
		return
	}

	userInfoBytes, _ := json.Marshal(userInfo)
	member := Member{UserID: userID, UserInfo: userInfoBytes}

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
		json.Unmarshal(m.UserInfo, &info)
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
		"event":   "pusher_internal:subscription_succeeded",
		"channel": channel,
		"data":    string(dataJson),
	})
	client.send <- successMsg

	if !alreadyPresent {
		addedMsg, _ := json.Marshal(map[string]interface{}{
			"event":   "pusher_internal:member_added",
			"channel": channel,
			"data":    string(userInfoBytes),
		})
		for otherClient := range sm.channels[channel] {
			if otherClient != client {
				select {
				case otherClient.send <- addedMsg:
				default:
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
	if strings.HasPrefix(channel, "presence-") {
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
				removedMsg, _ := json.Marshal(map[string]interface{}{
					"event":   "pusher_internal:member_removed",
					"channel": channel,
					"data":    fmt.Sprintf(`{"user_id":"%s"}`, userID),
				})
				if clients, ok := sm.channels[channel]; ok {
					for sub := range clients {
						select {
						case sub.send <- removedMsg:
						default:
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
