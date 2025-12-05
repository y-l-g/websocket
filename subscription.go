package websocket

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pogo/websocket/internal/protocol"
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

	sm.metrics.Subscriptions.Inc()

	if isNewChannel && sm.webhook != nil {
		sm.webhook.Notify("channel_occupied", channel)
	}

	if strings.HasPrefix(channel, "presence-") {
		sm.handlePresenceSubscribe(client, channel, userData)
	} else {
		msg, _ := json.Marshal(map[string]interface{}{
			"event":   protocol.EventSubscriptionSucceeded,
			"channel": channel,
			"data":    "{}",
		})
		client.send <- msg
	}
}

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

	if channelDataStr, ok := rawMap["channel_data"].(string); ok {
		var channelData map[string]interface{}
		if err := json.Unmarshal([]byte(channelDataStr), &channelData); err == nil {
			rawMap = channelData
		}
	}

	var userID string
	var userInfo interface{}

	if v, ok := rawMap["user_id"]; ok {
		userID = fmt.Sprintf("%v", v)
	}
	if v, ok := rawMap["user_info"]; ok {
		userInfo = v
	} else if v, ok := rawMap["info"]; ok {
		userInfo = v
	} else {
		userInfo = rawMap
	}

	if userID == "" {
		if v, ok := rawMap["user_info"].(map[string]interface{}); ok {
			if uid, exists := v["id"]; exists {
				userID = fmt.Sprintf("%v", uid)
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
		"event":   protocol.EventSubscriptionSucceeded,
		"channel": channel,
		"data":    string(dataJson),
	})
	client.send <- successMsg

	if !alreadyPresent {
		addedData := map[string]interface{}{
			"user_id":   userID,
			"user_info": userInfo,
		}
		addedDataBytes, _ := json.Marshal(addedData)
		addedMsg, _ := json.Marshal(map[string]interface{}{
			"event":   protocol.EventMemberAdded,
			"channel": channel,
			"data":    string(addedDataBytes),
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
				removedData := map[string]interface{}{
					"user_id": userID,
				}
				removedDataBytes, _ := json.Marshal(removedData)
				removedMsg, _ := json.Marshal(map[string]interface{}{
					"event":   protocol.EventMemberRemoved,
					"channel": channel,
					"data":    string(removedDataBytes),
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
