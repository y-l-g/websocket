package websocket

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

type WebhookManager struct {
	url     string
	secret  string
	client  *http.Client
	logger  *zap.Logger
	metrics *Metrics
	jobs    chan WebhookEvent
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

const (
	webhookMaxRetries       = 3
	webhookRetryBase        = 100 * time.Millisecond
	defaultWebhookQueueSize = 1024
)

type WebhookPayload struct {
	TimeMs int64          `json:"time_ms"`
	Events []WebhookEvent `json:"events"`
}

type WebhookEvent struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

func NewWebhookManager(logger *zap.Logger, url, secret string, metrics ...*Metrics) *WebhookManager {
	var m *Metrics
	if len(metrics) > 0 {
		m = metrics[0]
	}
	ctx, cancel := context.WithCancel(context.Background())
	wm := &WebhookManager{
		url:     url,
		secret:  secret,
		logger:  logger,
		metrics: m,
		jobs:    make(chan WebhookEvent, defaultWebhookQueueSize),
		ctx:     ctx,
		cancel:  cancel,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
	if url != "" {
		wm.wg.Add(1)
		go wm.run()
	}
	return wm
}

// Notify queues the event for delivery to Laravel.
func (wm *WebhookManager) Notify(eventName, channelName string) {
	if wm.url == "" {
		return
	}

	event := WebhookEvent{Name: eventName, Channel: channelName}
	var done <-chan struct{}
	if wm.ctx != nil {
		done = wm.ctx.Done()
	}
	select {
	case wm.jobs <- event:
		if wm.metrics != nil {
			wm.metrics.WebhookQueueDepth.Set(float64(len(wm.jobs)))
		}
	case <-done:
		if wm.metrics != nil {
			wm.metrics.WebhookDropped.WithLabelValues("closed").Inc()
		}
	default:
		if wm.metrics != nil {
			wm.metrics.WebhookDropped.WithLabelValues("queue_full").Inc()
		}
		wm.logger.Warn("Webhook: queue full, dropping notification", zap.String("event", eventName), zap.String("channel", channelName))
	}
}

func (wm *WebhookManager) run() {
	defer wm.wg.Done()
	for {
		select {
		case event := <-wm.jobs:
			if wm.metrics != nil {
				wm.metrics.WebhookQueueDepth.Set(float64(len(wm.jobs)))
			}
			wm.deliver(wm.ctx, event)
		case <-wm.ctx.Done():
			return
		}
	}
}

func (wm *WebhookManager) deliver(ctx context.Context, event WebhookEvent) {
	payload := WebhookPayload{
		TimeMs: time.Now().UnixMilli(),
		Events: []WebhookEvent{
			event,
		},
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		wm.logger.Error("Webhook: failed to marshal payload", zap.Error(err))
		return
	}

	signature := ""
	if wm.secret != "" {
		mac := hmac.New(sha256.New, []byte(wm.secret))
		mac.Write(jsonBytes)
		signature = hex.EncodeToString(mac.Sum(nil))
	}

	var lastErr error
	for attempt := 0; attempt <= webhookMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := webhookRetryBase * time.Duration(1<<uint(attempt-1))
			wm.logger.Warn("Webhook: retrying",
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
				zap.String("event", event.Name),
				zap.String("channel", event.Channel))
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", wm.url, bytes.NewReader(jsonBytes))
		if err != nil {
			wm.logger.Error("Webhook: failed to create request", zap.Error(err))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if signature != "" {
			req.Header.Set("X-Pusher-Key", "frankenphp")
			req.Header.Set("X-Pusher-Signature", signature)
		}

		resp, err := wm.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = resp.Body.Close()
			wm.logger.Debug("Webhook: sent", zap.String("event", event.Name), zap.String("channel", event.Channel))
			return
		}

		lastErr = fmt.Errorf("status %d", resp.StatusCode)
		_ = resp.Body.Close()
	}

	wm.logger.Error("Webhook: all retries exhausted",
		zap.Error(lastErr),
		zap.String("event", event.Name),
		zap.String("channel", event.Channel))
}

func (wm *WebhookManager) Close() {
	if wm.cancel == nil {
		return
	}
	wm.cancel()
	wm.wg.Wait()
}
