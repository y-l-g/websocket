package websocket

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxHTTPAPIRequestBody = 2 * 1024 * 1024
const maxHTTPSignatureAge = 5 * time.Minute

type pusherEventRequest struct {
	Name     string   `json:"name"`
	Data     string   `json:"data"`
	Channel  string   `json:"channel"`
	Channels []string `json:"channels"`
	SocketID string   `json:"socket_id"`
	Info     string   `json:"info"`
}

type pusherBatchRequest struct {
	Batch []pusherBatchEvent `json:"batch"`
}

type pusherBatchEvent struct {
	Name     string `json:"name"`
	Data     string `json:"data"`
	Channel  string `json:"channel"`
	SocketID string `json:"socket_id"`
	Info     string `json:"info"`
}

type pusherAPIRequest struct {
	AppID   string
	Action  string
	Channel string
	UserID  string
}

func (m *WebsocketModule) servePusherAPI(w http.ResponseWriter, r *http.Request) {
	apiRequest, ok := pusherAPIPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if apiRequest.AppID != m.AppID {
		writeJSONError(w, http.StatusNotFound, "application not found")
		return
	}

	expectedMethod := pusherAPIMethod(apiRequest.Action)
	if expectedMethod == "" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != expectedMethod {
		w.Header().Set("Allow", expectedMethod)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body := []byte{}
	if r.Method == http.MethodPost {
		var err error
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPAPIRequestBody))
		if err != nil {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
	}

	if !m.verifyPusherHTTPSignature(r, body) {
		writeJSONError(w, http.StatusUnauthorized, "authentication signature invalid")
		return
	}

	switch apiRequest.Action {
	case "events":
		m.handlePusherEvent(w, body)
	case "batch_events":
		m.handlePusherBatch(w, body)
	case "connections":
		m.handlePusherConnections(w)
	case "channels":
		m.handlePusherChannels(w, r)
	case "channel":
		m.handlePusherChannel(w, r, apiRequest.Channel)
	case "channel_users":
		m.handlePusherChannelUsers(w, apiRequest.Channel)
	case "users_terminate":
		m.handlePusherUserTerminate(w, apiRequest.UserID)
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

func pusherAPIPath(path string) (pusherAPIRequest, bool) {
	rest := strings.TrimPrefix(path, "/apps/")
	if rest == path {
		return pusherAPIRequest{}, false
	}

	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" {
		return pusherAPIRequest{}, false
	}

	request := pusherAPIRequest{AppID: parts[0]}

	switch {
	case len(parts) == 2 && parts[1] == "events":
		request.Action = "events"
	case len(parts) == 2 && parts[1] == "batch_events":
		request.Action = "batch_events"
	case len(parts) == 2 && parts[1] == "connections":
		request.Action = "connections"
	case len(parts) == 2 && parts[1] == "channels":
		request.Action = "channels"
	case len(parts) == 3 && parts[1] == "channels" && parts[2] != "":
		request.Action = "channel"
		request.Channel = parts[2]
	case len(parts) == 4 && parts[1] == "channels" && parts[2] != "" && parts[3] == "users":
		request.Action = "channel_users"
		request.Channel = parts[2]
	case len(parts) == 4 && parts[1] == "users" && parts[2] != "" && parts[3] == "terminate_connections":
		request.Action = "users_terminate"
		request.UserID = parts[2]
	default:
		return pusherAPIRequest{}, false
	}

	return request, true
}

func pusherAPIMethod(action string) string {
	switch action {
	case "events", "batch_events", "users_terminate":
		return http.MethodPost
	case "connections", "channels", "channel", "channel_users":
		return http.MethodGet
	default:
		return ""
	}
}

func (m *WebsocketModule) verifyPusherHTTPSignature(r *http.Request, body []byte) bool {
	query := r.URL.Query()
	if query.Get("auth_key") != m.AppKey {
		return false
	}
	if query.Get("auth_version") != "1.0" {
		return false
	}

	timestamp, err := strconv.ParseInt(query.Get("auth_timestamp"), 10, 64)
	if err != nil || timestamp <= 0 {
		return false
	}

	signedAt := time.Unix(timestamp, 0)
	now := time.Now()
	if now.Sub(signedAt) > maxHTTPSignatureAge || signedAt.Sub(now) > maxHTTPSignatureAge {
		return false
	}

	authSignature := query.Get("auth_signature")
	if authSignature == "" {
		return false
	}

	params := make(map[string]string, len(query)+1)
	for key, values := range query {
		switch key {
		case "auth_signature", "body_md5", "appId", "appKey", "channelName":
			continue
		}
		params[key] = strings.Join(values, ",")
	}

	if len(body) > 0 {
		sum := md5.Sum(body)
		params["body_md5"] = hex.EncodeToString(sum[:])
	}

	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+params[key])
	}

	toSign := strings.Join([]string{
		r.Method,
		r.URL.Path,
		strings.Join(pairs, "&"),
	}, "\n")

	mac := hmac.New(sha256.New, []byte(m.AppSecret))
	mac.Write([]byte(toSign))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(authSignature))
}

func (m *WebsocketModule) handlePusherEvent(w http.ResponseWriter, body []byte) {
	var request pusherEventRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}

	channels := request.Channels
	if len(channels) == 0 && request.Channel != "" {
		channels = []string{request.Channel}
	}
	if request.Name == "" || request.Data == "" || len(channels) == 0 {
		writeJSONError(w, http.StatusUnprocessableEntity, "name, data, and channel or channels are required")
		return
	}

	options := PublishOptions{ExceptSocketID: request.SocketID}
	for _, channel := range channels {
		if channel == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "channel must not be empty")
			return
		}
		if status := publishToActiveHubsWithOptions(GetHubs(m.AppID), channel, request.Name, request.Data, options); status != PublishOK {
			writePublishError(w, status)
			return
		}
	}

	if request.Info != "" {
		writeJSON(w, http.StatusOK, map[string]any{"channels": channelResponses(m.AppID, channels, request.Info)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{})
}

func (m *WebsocketModule) handlePusherBatch(w http.ResponseWriter, body []byte) {
	var request pusherBatchRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(request.Batch) == 0 {
		writeJSONError(w, http.StatusUnprocessableEntity, "batch is required")
		return
	}

	hasInfo := false
	infoResponses := make([]map[string]any, 0, len(request.Batch))
	for _, item := range request.Batch {
		if item.Name == "" || item.Data == "" || item.Channel == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "each batch item requires name, data, and channel")
			return
		}
		if item.Info != "" {
			hasInfo = true
			infoResponses = append(infoResponses, channelSnapshotResponse(
				collectChannelSnapshot(GetHubs(m.AppID), item.Channel),
				parseInfo(item.Info),
				false,
			))
		} else {
			infoResponses = append(infoResponses, map[string]any{})
		}
		options := PublishOptions{ExceptSocketID: item.SocketID}
		if status := publishToActiveHubsWithOptions(GetHubs(m.AppID), item.Channel, item.Name, item.Data, options); status != PublishOK {
			writePublishError(w, status)
			return
		}
	}

	if hasInfo {
		writeJSON(w, http.StatusOK, map[string]any{"batch": infoResponses})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"batch": map[string]any{}})
}

func channelResponses(appID string, channels []string, info string) map[string]map[string]any {
	responses := make(map[string]map[string]any, len(channels))
	parsedInfo := parseInfo(info)
	for _, channel := range channels {
		responses[channel] = channelSnapshotResponse(collectChannelSnapshot(GetHubs(appID), channel), parsedInfo, false)
	}
	return responses
}

func (m *WebsocketModule) handlePusherConnections(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]int{
		"connections": countConnections(GetHubs(m.AppID)),
	})
}

func (m *WebsocketModule) handlePusherChannels(w http.ResponseWriter, r *http.Request) {
	info := parseInfo(r.URL.Query().Get("info"))
	channels := map[string]map[string]any{}

	for _, snapshot := range collectChannelSnapshots(GetHubs(m.AppID), r.URL.Query().Get("filter_by_prefix")) {
		channels[snapshot.Name] = channelSnapshotResponse(snapshot, info, false)
	}

	writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

func (m *WebsocketModule) handlePusherChannel(w http.ResponseWriter, r *http.Request, channel string) {
	snapshot := collectChannelSnapshot(GetHubs(m.AppID), channel)
	writeJSON(w, http.StatusOK, channelSnapshotResponse(snapshot, parseInfo(r.URL.Query().Get("info")), true))
}

func (m *WebsocketModule) handlePusherChannelUsers(w http.ResponseWriter, channel string) {
	snapshot := collectChannelSnapshot(GetHubs(m.AppID), channel)
	if snapshot.SubscriptionCount == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{})
		return
	}
	if !snapshot.Presence {
		writeJSON(w, http.StatusBadRequest, map[string]any{})
		return
	}

	users := make([]map[string]string, 0, len(snapshot.UserIDs))
	for _, userID := range snapshot.UserIDs {
		users = append(users, map[string]string{"id": userID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (m *WebsocketModule) handlePusherUserTerminate(w http.ResponseWriter, userID string) {
	for _, hub := range GetHubs(m.AppID) {
		hub.TerminateUserConnections(userID)
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func countConnections(hubs []*Hub) int {
	ids := map[string]struct{}{}
	for _, hub := range hubs {
		for _, id := range hub.ConnectionIDs() {
			ids[id] = struct{}{}
		}
	}
	return len(ids)
}

func collectChannelSnapshots(hubs []*Hub, filterByPrefix string) []ChannelSnapshot {
	combined := map[string]ChannelSnapshot{}
	for _, hub := range hubs {
		for _, snapshot := range hub.ChannelSnapshots(filterByPrefix) {
			combined[snapshot.Name] = mergeChannelSnapshots(combined[snapshot.Name], snapshot)
		}
	}

	snapshots := make([]ChannelSnapshot, 0, len(combined))
	for _, snapshot := range combined {
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Name < snapshots[j].Name
	})
	return snapshots
}

func collectChannelSnapshot(hubs []*Hub, channel string) ChannelSnapshot {
	combined := ChannelSnapshot{Name: channel}
	for _, hub := range hubs {
		combined = mergeChannelSnapshots(combined, hub.ChannelSnapshot(channel))
	}
	combined.Name = channel
	return combined
}

func mergeChannelSnapshots(left ChannelSnapshot, right ChannelSnapshot) ChannelSnapshot {
	if left.Name == "" {
		left.Name = right.Name
	}
	left.SubscriptionCount += right.SubscriptionCount
	left.Presence = left.Presence || right.Presence

	if len(right.UserIDs) > 0 {
		users := make(map[string]struct{}, len(left.UserIDs)+len(right.UserIDs))
		for _, userID := range left.UserIDs {
			users[userID] = struct{}{}
		}
		for _, userID := range right.UserIDs {
			users[userID] = struct{}{}
		}
		left.UserIDs = left.UserIDs[:0]
		for userID := range users {
			left.UserIDs = append(left.UserIDs, userID)
		}
		sort.Strings(left.UserIDs)
	}

	return left
}

func parseInfo(info string) map[string]bool {
	values := map[string]bool{}
	for _, value := range strings.Split(info, ",") {
		value = strings.TrimSpace(value)
		if value != "" {
			values[value] = true
		}
	}
	return values
}

func channelSnapshotResponse(snapshot ChannelSnapshot, info map[string]bool, includeOccupied bool) map[string]any {
	response := map[string]any{}
	occupied := snapshot.SubscriptionCount > 0
	if includeOccupied {
		response["occupied"] = occupied
	}
	if !occupied {
		return response
	}

	if snapshot.Presence {
		if info["user_count"] {
			response["user_count"] = len(snapshot.UserIDs)
		}
		return response
	}

	if info["subscription_count"] {
		response["subscription_count"] = snapshot.SubscriptionCount
	}
	return response
}

func writePublishError(w http.ResponseWriter, status PublishStatus) {
	writeJSONError(w, httpStatusForPublishStatus(status), fmt.Sprintf("publish failed: %s", publishStatusReason(status)))
}

func publishStatusReason(status PublishStatus) string {
	switch status {
	case PublishHubMissing:
		return "hub_missing"
	case PublishChannelTooLong:
		return "channel_too_long"
	case PublishEventTooLong:
		return "event_too_long"
	case PublishPayloadTooLarge:
		return "payload_too_large"
	case PublishInvalidPayloadJSON:
		return "invalid_payload_json"
	case PublishBrokerFailed:
		return "broker_failed"
	case PublishInvalidChannelsJSON:
		return "invalid_channels_json"
	case PublishBrokerQueueFull:
		return "broker_queue_full"
	case PublishShardQueueFull:
		return "shard_queue_full"
	case PublishOK:
		return "ok"
	default:
		return "unknown"
	}
}

func httpStatusForPublishStatus(status PublishStatus) int {
	switch status {
	case PublishHubMissing:
		return http.StatusNotFound
	case PublishChannelTooLong, PublishEventTooLong, PublishPayloadTooLarge, PublishInvalidPayloadJSON, PublishInvalidChannelsJSON:
		return http.StatusUnprocessableEntity
	case PublishBrokerQueueFull, PublishShardQueueFull:
		return http.StatusServiceUnavailable
	case PublishBrokerFailed:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
