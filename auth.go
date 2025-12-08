package websocket

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

// RequestDispatcher allows mocking the FrankenPHP worker
type RequestDispatcher interface {
	SendRequest(w http.ResponseWriter, r *http.Request) error
}

const (
	CBThreshold    = 5
	CBResetTimeout = 10 * time.Second
)

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

type AuthResult struct {
	Allowed  bool
	UserData json.RawMessage
}

type AuthProvider interface {
	Authorize(client *Client, channel string) AuthResult
	AuthenticateUser(client *Client, authSig string, userData string) AuthResult
}

type WorkerAuthProvider struct {
	worker      RequestDispatcher
	authPath    string
	logger      *zap.Logger
	metrics     *Metrics
	breaker     *gobreaker.CircuitBreaker
	maxAuthBody int
	sem         chan struct{}
	secret      string // App Secret for local verification
}

func NewWorkerAuthProvider(logger *zap.Logger, metrics *Metrics, worker RequestDispatcher, authPath string, maxAuthBody int, maxConcurrent int, secret string) *WorkerAuthProvider {
	st := gobreaker.Settings{
		Name:        "FrankenPHP-Auth-Worker",
		MaxRequests: 1,
		Interval:    0,
		Timeout:     CBResetTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= CBThreshold
		},
	}

	if maxConcurrent <= 0 {
		maxConcurrent = 100
	}

	return &WorkerAuthProvider{
		logger:      logger,
		metrics:     metrics,
		worker:      worker,
		authPath:    authPath,
		breaker:     gobreaker.NewCircuitBreaker(st),
		maxAuthBody: maxAuthBody,
		sem:         make(chan struct{}, maxConcurrent),
		secret:      secret,
	}
}

// AuthenticateUser performs local HMAC verification of the pusher:signin event.
// Ref: https://pusher.com/docs/channels/server_api/authenticating-users/
func (ap *WorkerAuthProvider) AuthenticateUser(client *Client, authSig string, userData string) AuthResult {
	if ap.secret == "" {
		ap.logger.Warn("Auth: user authentication failed, no secret configured")
		return AuthResult{Allowed: false}
	}

	// Format: key:signature
	parts := strings.Split(authSig, ":")
	if len(parts) != 2 {
		return AuthResult{Allowed: false}
	}

	// We ignore the key part (parts[0]) here and validate using our configured secret.
	// In a multi-tenant system, we would look up secret by key.
	signature := parts[1]

	// String to sign: socket_id + "::user::" + user_data
	toSign := fmt.Sprintf("%s::user::%s", client.ID, userData)

	mac := hmac.New(sha256.New, []byte(ap.secret))
	mac.Write([]byte(toSign))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if hmac.Equal([]byte(signature), []byte(expectedSig)) {
		// Valid
		return AuthResult{Allowed: true, UserData: json.RawMessage(userData)}
	}

	ap.logger.Warn("Auth: user signature mismatch", zap.String("id", client.ID))
	return AuthResult{Allowed: false}
}

func (ap *WorkerAuthProvider) Authorize(client *Client, channel string) AuthResult {
	select {
	case ap.sem <- struct{}{}:
		defer func() { <-ap.sem }()
	default:
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("concurrency_limit").Inc()
		}
		ap.logger.Warn("Auth: concurrency limit reached", zap.String("id", client.ID))
		return AuthResult{Allowed: false}
	}

	result, err := ap.breaker.Execute(func() (interface{}, error) {
		return ap.doAuthorize(client, channel)
	})

	if err != nil {
		switch err {
		case gobreaker.ErrOpenState:
			ap.metrics.BreakerTripped.Inc()
		case gobreaker.ErrTooManyRequests:
			ap.metrics.BreakerTripped.Inc()
		}
		return AuthResult{Allowed: false}
	}

	return result.(AuthResult)
}

func (ap *WorkerAuthProvider) doAuthorize(client *Client, channel string) (AuthResult, error) {
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
		return AuthResult{Allowed: false}, nil
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
		return AuthResult{Allowed: false}, errors.New("worker not initialized")
	}

	err = ap.worker.SendRequest(rr, req.WithContext(ctx))

	if err != nil {
		ap.metrics.AuthFailures.WithLabelValues("dispatch_error").Inc()
		ap.logger.Error("Auth: worker dispatch failed", zap.Error(err))
		return AuthResult{Allowed: false}, err
	}

	if rr.overflow {
		ap.metrics.AuthFailures.WithLabelValues("body_overflow").Inc()
		ap.logger.Warn("Auth: response body too large")
		return AuthResult{Allowed: false}, nil
	}

	if rr.status >= 500 {
		ap.metrics.AuthFailures.WithLabelValues("worker_error").Inc()
		ap.logger.Warn("Auth: worker error", zap.Int("status", rr.status))
		return AuthResult{Allowed: false}, errors.New("worker 500")
	}

	if rr.status != 200 {
		return AuthResult{Allowed: false}, nil
	}

	body := make([]byte, rr.body.Len())
	copy(body, rr.body.Bytes())

	return AuthResult{Allowed: true, UserData: body}, nil
}
