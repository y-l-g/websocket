package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/dunglas/frankenphp"
	"go.uber.org/zap"
)

const (
	AuthCacheTTL             = 30 * time.Second
	AuthCacheCleanupInterval = 60 * time.Second
	CBThreshold              = 5
	CBResetTimeout           = 10 * time.Second
)

type CircuitBreaker struct {
	mu           sync.RWMutex
	failures     int
	lastFailure  time.Time
	threshold    int
	resetTimeout time.Duration
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		threshold:    CBThreshold,
		resetTimeout: CBResetTimeout,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if cb.failures >= cb.threshold {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			return true
		}
		return false
	}
	return true
}

func (cb *CircuitBreaker) Report(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if success {
		cb.failures = 0
	} else {
		cb.failures++
		cb.lastFailure = time.Now()
	}
}

type AuthResult struct {
	Allowed  bool
	UserData json.RawMessage
}

type CachedAuth struct {
	Result    AuthResult
	ExpiresAt time.Time
}

type AuthProvider interface {
	Authorize(client *Client, channel string) AuthResult
}

type WorkerAuthProvider struct {
	worker   frankenphp.Workers
	authPath string
	logger   *zap.Logger
	cache    map[string]CachedAuth
	cacheMu  sync.RWMutex
	breaker  *CircuitBreaker
}

func NewWorkerAuthProvider(logger *zap.Logger, worker frankenphp.Workers, authPath string) *WorkerAuthProvider {
	ap := &WorkerAuthProvider{
		logger:   logger,
		worker:   worker,
		authPath: authPath,
		cache:    make(map[string]CachedAuth),
		breaker:  NewCircuitBreaker(),
	}
	go ap.cleanupLoop()
	return ap
}

func (ap *WorkerAuthProvider) cleanupLoop() {
	ticker := time.NewTicker(AuthCacheCleanupInterval)
	for range ticker.C {
		ap.cacheMu.Lock()
		now := time.Now()
		for k, v := range ap.cache {
			if now.After(v.ExpiresAt) {
				delete(ap.cache, k)
			}
		}
		ap.cacheMu.Unlock()
	}
}

func (ap *WorkerAuthProvider) Authorize(client *Client, channel string) AuthResult {
	// 1. Check Cache
	cookie := client.Headers.Get("Cookie")
	var cacheKey string
	if cookie != "" {
		cacheKey = channel + "|" + cookie
		ap.cacheMu.RLock()
		cached, ok := ap.cache[cacheKey]
		ap.cacheMu.RUnlock()

		if ok && time.Now().Before(cached.ExpiresAt) {
			ap.logger.Debug("Auth: cache hit", zap.String("channel", channel))
			return cached.Result
		}
	}

	// 2. Check Circuit Breaker
	if !ap.breaker.Allow() {
		metricBreakerTripped.Inc() // Metric
		ap.logger.Warn("Auth: circuit open, failing fast", zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	// 3. Measure Duration
	start := time.Now()
	defer func() {
		metricAuthDuration.Observe(time.Since(start).Seconds()) // Metric
	}()

	bodyData := map[string]string{
		"channel_name": channel,
		"socket_id":    client.ID,
	}
	jsonBytes, _ := json.Marshal(bodyData)

	req, err := http.NewRequest("POST", ap.authPath, bytes.NewBuffer(jsonBytes))
	if err != nil {
		ap.logger.Error("Auth: failed to create request", zap.Error(err))
		return AuthResult{Allowed: false}
	}

	for k, v := range client.Headers {
		req.Header[k] = v
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-FrankenPHP-WS-Channel", channel)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rr := httptest.NewRecorder()

	if ap.worker == nil {
		ap.logger.Error("Auth: worker not initialized")
		return AuthResult{Allowed: false}
	}

	err = ap.worker.SendRequest(rr, req.WithContext(ctx))

	if err != nil {
		ap.breaker.Report(false)
		metricAuthFailures.Inc() // Metric
		ap.logger.Error("Auth: worker dispatch failed", zap.Error(err))
		return AuthResult{Allowed: false}
	}

	if rr.Code >= 500 {
		ap.breaker.Report(false)
		metricAuthFailures.Inc() // Metric
		ap.logger.Warn("Auth: worker error", zap.Int("status", rr.Code))
		return AuthResult{Allowed: false}
	}

	// 403 Forbidden is a successful "Response", just an auth denial.
	ap.breaker.Report(true)

	if rr.Code != 200 {
		return AuthResult{Allowed: false}
	}

	body, _ := io.ReadAll(rr.Body)
	result := AuthResult{
		Allowed:  true,
		UserData: body,
	}

	if cookie != "" {
		ap.cacheMu.Lock()
		ap.cache[cacheKey] = CachedAuth{
			Result:    result,
			ExpiresAt: time.Now().Add(AuthCacheTTL),
		}
		ap.cacheMu.Unlock()
	}

	return result
}
