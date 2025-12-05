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
	CBThreshold    = 5
	CBResetTimeout = 10 * time.Second
)

var recorderPool = sync.Pool{
	New: func() interface{} {
		return httptest.NewRecorder()
	},
}

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

type AuthProvider interface {
	Authorize(client *Client, channel string) AuthResult
}

type WorkerAuthProvider struct {
	worker   frankenphp.Workers
	authPath string
	logger   *zap.Logger
	metrics  *Metrics
	breaker  *CircuitBreaker
}

func NewWorkerAuthProvider(logger *zap.Logger, metrics *Metrics, worker frankenphp.Workers, authPath string) *WorkerAuthProvider {
	return &WorkerAuthProvider{
		logger:   logger,
		metrics:  metrics,
		worker:   worker,
		authPath: authPath,
		breaker:  NewCircuitBreaker(),
	}
}

func (ap *WorkerAuthProvider) Authorize(client *Client, channel string) AuthResult {
	cookie := client.Headers.Get("Cookie")
	if cookie == "" {
		ap.logger.Debug("Auth: No cookie received from client", zap.String("id", client.ID))
	}

	if !ap.breaker.Allow() {
		ap.metrics.BreakerTripped.Inc()
		ap.logger.Warn("Auth: circuit open, failing fast", zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	start := time.Now()
	defer func() {
		ap.metrics.AuthDuration.Observe(time.Since(start).Seconds())
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

	rr := recorderPool.Get().(*httptest.ResponseRecorder)
	defer func() {
		rr.Body.Reset()
		rr.HeaderMap = make(http.Header)
		rr.Code = 200
		rr.Flushed = false
		recorderPool.Put(rr)
	}()

	if ap.worker == nil {
		ap.logger.Error("Auth: worker not initialized")
		return AuthResult{Allowed: false}
	}

	err = ap.worker.SendRequest(rr, req.WithContext(ctx))

	if err != nil {
		ap.breaker.Report(false)
		ap.metrics.AuthFailures.Inc()
		ap.logger.Error("Auth: worker dispatch failed", zap.Error(err))
		return AuthResult{Allowed: false}
	}

	if rr.Code >= 500 {
		ap.breaker.Report(false)
		ap.metrics.AuthFailures.Inc()
		ap.logger.Warn("Auth: worker error", zap.Int("status", rr.Code))
		return AuthResult{Allowed: false}
	}

	ap.breaker.Report(true)

	if rr.Code != 200 {
		return AuthResult{Allowed: false}
	}

	body, _ := io.ReadAll(rr.Body)

	return AuthResult{
		Allowed:  true,
		UserData: body,
	}
}
