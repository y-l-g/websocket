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
	"strings"
)

const maxHTTPAPIRequestBody = 2 * 1024 * 1024

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

func (m *WebsocketModule) servePusherAPI(w http.ResponseWriter, r *http.Request) {
	appID, action, ok := pusherAPIPath(r.URL.Path)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if appID != m.AppID {
		writeJSONError(w, http.StatusNotFound, "application not found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPAPIRequestBody))
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}

	if !m.verifyPusherHTTPSignature(r, body) {
		writeJSONError(w, http.StatusUnauthorized, "authentication signature invalid")
		return
	}

	switch action {
	case "events":
		m.handlePusherEvent(w, body)
	case "batch_events":
		m.handlePusherBatch(w, body)
	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

func pusherAPIPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, "/apps/")
	if rest == path {
		return "", "", false
	}

	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}

	return parts[0], parts[1], true
}

func (m *WebsocketModule) verifyPusherHTTPSignature(r *http.Request, body []byte) bool {
	query := r.URL.Query()
	if query.Get("auth_key") != m.AppKey {
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
		writeJSON(w, http.StatusOK, map[string]any{"channels": []map[string]any{}})
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
	for _, item := range request.Batch {
		if item.Name == "" || item.Data == "" || item.Channel == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "each batch item requires name, data, and channel")
			return
		}
		if item.Info != "" {
			hasInfo = true
		}
		options := PublishOptions{ExceptSocketID: item.SocketID}
		if status := publishToActiveHubsWithOptions(GetHubs(m.AppID), item.Channel, item.Name, item.Data, options); status != PublishOK {
			writePublishError(w, status)
			return
		}
	}

	if hasInfo {
		writeJSON(w, http.StatusOK, map[string]any{"batch": []map[string]any{}})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"batch": map[string]any{}})
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
