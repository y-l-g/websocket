package websocket

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/dunglas/frankenphp"
	"go.uber.org/zap"
)

const (
	CBThreshold    = 5
	CBResetTimeout = 10 * time.Second

	StateClosed = iota
	StateOpen
	StateHalfOpen
)

// ... responseCapturer struct and pool (Unchanged) ...

type responseCapturer struct {
	status   int
	header   http.Header
	body     bytes.Buffer
	overflow bool
	maxSize  int
}

func (r *responseCapturer) Header() http.Header { return r.header }
func (r *responseCapturer) Write(b []byte) (int, error) {
	if r.overflow {
		return len(b), nil
	}
	if r.body.Len()+len(b) > r.maxSize {
		r.overflow = true
		return len(b), nil
	}
	return r.body.Write(b)
}
func (r *responseCapturer) WriteHeader(statusCode int) { r.status = statusCode }

var capturerPool = sync.Pool{
	New: func() interface{} {
		return &responseCapturer{header: make(http.Header), status: 200}
	},
}

type CircuitBreaker struct {
	mu           sync.RWMutex
	state        int
	failures     int
	lastFailure  time.Time
	threshold    int
	resetTimeout time.Duration
}

func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		state:        StateClosed,
		threshold:    CBThreshold,
		resetTimeout: CBResetTimeout,
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailure
	cb.mu.RUnlock()

	if state == StateClosed {
		return true
	}

	if state == StateOpen {
		if time.Since(lastFailure) > cb.resetTimeout {
			cb.mu.Lock()
			defer cb.mu.Unlock()
			if cb.state == StateOpen && time.Since(cb.lastFailure) > cb.resetTimeout {
				cb.state = StateHalfOpen
				return true
			}
			if cb.state == StateHalfOpen {
				return false
			}
		}
		return false
	}
	return false
}

func (cb *CircuitBreaker) Report(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if success {
		switch cb.state {
		case StateHalfOpen:
			cb.state = StateClosed
			cb.failures = 0
		case StateClosed:
			cb.failures = 0
		}
	} else {
		switch cb.state {
		case StateHalfOpen:
			cb.state = StateOpen
			cb.lastFailure = time.Now()
		case StateClosed:
			cb.failures++
			if cb.failures >= cb.threshold {
				cb.state = StateOpen
				cb.lastFailure = time.Now()
			}
		case StateOpen:
			cb.lastFailure = time.Now()
		}
	}
}

// ... rest of auth.go (AuthProvider interfaces, WorkerAuthProvider) Unchanged ...

type AuthResult struct {
	Allowed  bool
	UserData json.RawMessage
}

type AuthProvider interface {
	Authorize(client *Client, channel string) AuthResult
}

type WorkerAuthProvider struct {
	worker      frankenphp.Workers
	authPath    string
	logger      *zap.Logger
	metrics     *Metrics
	breaker     *CircuitBreaker
	maxAuthBody int
}

func NewWorkerAuthProvider(logger *zap.Logger, metrics *Metrics, worker frankenphp.Workers, authPath string, maxAuthBody int) *WorkerAuthProvider {
	return &WorkerAuthProvider{
		logger:      logger,
		metrics:     metrics,
		worker:      worker,
		authPath:    authPath,
		breaker:     NewCircuitBreaker(),
		maxAuthBody: maxAuthBody,
	}
}

func (ap *WorkerAuthProvider) Authorize(client *Client, channel string) AuthResult {
	if !ap.breaker.Allow() {
		ap.metrics.BreakerTripped.Inc()
		ap.logger.Warn("Auth: circuit open", zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	start := time.Now()
	defer func() { ap.metrics.AuthDuration.Observe(time.Since(start).Seconds()) }()

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

	rr := capturerPool.Get().(*responseCapturer)
	rr.maxSize = ap.maxAuthBody

	defer func() {
		rr.body.Reset()
		for k := range rr.header {
			delete(rr.header, k)
		}
		rr.status = 200
		rr.overflow = false
		capturerPool.Put(rr)
	}()

	if ap.worker == nil {
		ap.logger.Error("Auth: worker not initialized")
		return AuthResult{Allowed: false}
	}

	err = ap.worker.SendRequest(rr, req.WithContext(ctx))

	if err != nil {
		ap.breaker.Report(false)
		ap.metrics.AuthFailures.WithLabelValues("dispatch_error").Inc()
		ap.logger.Error("Auth: worker dispatch failed", zap.Error(err))
		return AuthResult{Allowed: false}
	}

	if rr.overflow {
		ap.breaker.Report(false)
		ap.metrics.AuthFailures.WithLabelValues("body_overflow").Inc()
		ap.logger.Warn("Auth: response body too large")
		return AuthResult{Allowed: false}
	}

	if rr.status >= 500 {
		ap.breaker.Report(false)
		ap.metrics.AuthFailures.WithLabelValues("worker_error").Inc()
		ap.logger.Warn("Auth: worker error", zap.Int("status", rr.status))
		return AuthResult{Allowed: false}
	}

	ap.breaker.Report(true)

	if rr.status != 200 {
		return AuthResult{Allowed: false}
	}

	body := make([]byte, rr.body.Len())
	copy(body, rr.body.Bytes())

	return AuthResult{Allowed: true, UserData: body}
}
