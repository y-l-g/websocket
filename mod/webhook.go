package websocket

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type WebhookManager struct {
	url    string
	secret string
	client *http.Client
	logger *zap.Logger
}

type WebhookPayload struct {
	TimeMs int64          `json:"time_ms"`
	Events []WebhookEvent `json:"events"`
}

type WebhookEvent struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

func NewWebhookManager(logger *zap.Logger, url, secret string) *WebhookManager {
	return &WebhookManager{
		url:    url,
		secret: secret,
		logger: logger,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Notify sends the event to Laravel in a separate goroutine
func (wm *WebhookManager) Notify(eventName, channelName string) {
	if wm.url == "" {
		return
	}

	// Clone data for async execution
	go func(evt, ch string) {
		payload := WebhookPayload{
			TimeMs: time.Now().UnixMilli(),
			Events: []WebhookEvent{
				{Name: evt, Channel: ch},
			},
		}

		jsonBytes, _ := json.Marshal(payload)
		req, err := http.NewRequest("POST", wm.url, bytes.NewBuffer(jsonBytes))
		if err != nil {
			wm.logger.Error("Webhook: failed to create request", zap.Error(err))
			return
		}

		req.Header.Set("Content-Type", "application/json")

		// Pusher-compatible Signature
		if wm.secret != "" {
			mac := hmac.New(sha256.New, []byte(wm.secret))
			mac.Write(jsonBytes)
			signature := hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-Pusher-Key", "frankenphp") // Dummy key
			req.Header.Set("X-Pusher-Signature", signature)
		}

		resp, err := wm.client.Do(req)
		if err != nil {
			wm.logger.Error("Webhook: request failed", zap.Error(err))
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != 200 {
			wm.logger.Warn("Webhook: non-200 response", zap.Int("status", resp.StatusCode))
		} else {
			wm.logger.Debug("Webhook: sent", zap.String("event", evt), zap.String("channel", ch))
		}
	}(eventName, channelName)
}
