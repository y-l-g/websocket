package websocket

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

const (
	webhookMaxRetries = 3
	webhookRetryBase  = 100 * time.Millisecond
)

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

// Notify sends the event to Laravel in a separate goroutine with retry
func (wm *WebhookManager) Notify(eventName, channelName string) {
	if wm.url == "" {
		return
	}

	go func(evt, ch string) {
		payload := WebhookPayload{
			TimeMs: time.Now().UnixMilli(),
			Events: []WebhookEvent{
				{Name: evt, Channel: ch},
			},
		}

		jsonBytes, err := json.Marshal(payload)
		if err != nil {
			wm.logger.Error("Webhook: failed to marshal payload", zap.Error(err))
			return
		}

		req, err := http.NewRequest("POST", wm.url, bytes.NewBuffer(jsonBytes))
		if err != nil {
			wm.logger.Error("Webhook: failed to create request", zap.Error(err))
			return
		}

		req.Header.Set("Content-Type", "application/json")

		if wm.secret != "" {
			mac := hmac.New(sha256.New, []byte(wm.secret))
			mac.Write(jsonBytes)
			signature := hex.EncodeToString(mac.Sum(nil))
			req.Header.Set("X-Pusher-Key", "frankenphp")
			req.Header.Set("X-Pusher-Signature", signature)
		}

		var lastErr error
		for attempt := 0; attempt <= webhookMaxRetries; attempt++ {
			if attempt > 0 {
				backoff := webhookRetryBase * time.Duration(1<<uint(attempt-1))
				wm.logger.Warn("Webhook: retrying",
					zap.Int("attempt", attempt),
					zap.Duration("backoff", backoff),
					zap.String("event", evt),
					zap.String("channel", ch))
				time.Sleep(backoff)
			}

			resp, err := wm.client.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				_ = resp.Body.Close()
				wm.logger.Debug("Webhook: sent", zap.String("event", evt), zap.String("channel", ch))
				return
			}

			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			_ = resp.Body.Close()
		}

		wm.logger.Error("Webhook: all retries exhausted",
			zap.Error(lastErr),
			zap.String("event", evt),
			zap.String("channel", ch))
	}(eventName, channelName)
}
