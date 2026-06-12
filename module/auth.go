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

	"github.com/sony/gobreaker/v2"
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
	Authorize(client *Client, channel string, auth string, channelData string) AuthResult
	AuthenticateUser(client *Client, authSig string, userData string) AuthResult
}

type channelAuthResponse struct {
	Auth        string `json:"auth"`
	ChannelData string `json:"channel_data,omitempty"`
}

type WorkerAuthProvider struct {
	appKey      string
	worker      RequestDispatcher
	authPath    string
	logger      *zap.Logger
	metrics     *Metrics
	breaker     *gobreaker.CircuitBreaker[AuthResult]
	maxAuthBody int
	sem         chan struct{}
	secret      string
}

func NewWorkerAuthProvider(logger *zap.Logger, metrics *Metrics, worker RequestDispatcher, appKey string, authPath string, maxAuthBody int, maxConcurrent int, secret string) *WorkerAuthProvider {
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
		appKey:      appKey,
		logger:      logger,
		metrics:     metrics,
		worker:      worker,
		authPath:    authPath,
		breaker:     gobreaker.NewCircuitBreaker[AuthResult](st),
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
	if parts[0] != ap.appKey {
		ap.logger.Warn("Auth: user signature app key mismatch", zap.String("id", client.ID))
		return AuthResult{Allowed: false}
	}

	// String to sign: socket_id + "::user::" + user_data
	toSign := fmt.Sprintf("%s::user::%s", client.ID, userData)

	if validSignature(ap.secret, parts[1], toSign) {
		// Valid
		return AuthResult{Allowed: true, UserData: json.RawMessage(userData)}
	}

	ap.logger.Warn("Auth: user signature mismatch", zap.String("id", client.ID))
	return AuthResult{Allowed: false}
}

func (ap *WorkerAuthProvider) Authorize(client *Client, channel string, auth string, channelData string) AuthResult {
	if auth != "" {
		return ap.authorizeProvidedSignature(client, channel, auth, channelData)
	}
	if ap.worker == nil {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("missing_signature").Inc()
		}
		ap.logger.Warn("Auth: subscription auth signature missing and no auth worker configured", zap.String("id", client.ID), zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

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

	result, err := ap.breaker.Execute(func() (AuthResult, error) { // Change return type to AuthResult
		return ap.doAuthorize(client, channel)
	})

	if err != nil {
		switch err {
		case gobreaker.ErrOpenState:
			ap.metrics.BreakerTripped.Inc()
		case gobreaker.ErrTooManyRequests:
			ap.metrics.BreakerTripped.Inc()
		}
		return result
	}

	return result
}

func (ap *WorkerAuthProvider) authorizeProvidedSignature(client *Client, channel string, auth string, channelData string) AuthResult {
	if strings.HasPrefix(channel, "presence-") && channelData == "" {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("missing_channel_data").Inc()
		}
		ap.logger.Warn("Auth: presence channel auth missing channel_data", zap.String("id", client.ID), zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	if !ap.validateChannelSignature(client.ID, channel, auth, channelData) {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("invalid_signature").Inc()
		}
		ap.logger.Warn("Auth: channel signature mismatch", zap.String("id", client.ID), zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	raw, err := json.Marshal(channelAuthResponse{Auth: auth, ChannelData: channelData})
	if err != nil {
		return AuthResult{Allowed: false}
	}
	return AuthResult{Allowed: true, UserData: raw}
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

	result := ap.validateWorkerAuthResponse(client, channel, body)
	return result, nil
}

func (ap *WorkerAuthProvider) validateWorkerAuthResponse(client *Client, channel string, body []byte) AuthResult {
	var response channelAuthResponse
	if err := json.Unmarshal(body, &response); err != nil {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("invalid_response_json").Inc()
		}
		ap.logger.Warn("Auth: worker returned invalid JSON", zap.String("id", client.ID), zap.Error(err))
		return AuthResult{Allowed: false}
	}

	if response.Auth == "" {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("missing_signature").Inc()
		}
		ap.logger.Warn("Auth: worker response missing auth signature", zap.String("id", client.ID), zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	if strings.HasPrefix(channel, "presence-") && response.ChannelData == "" {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("missing_channel_data").Inc()
		}
		ap.logger.Warn("Auth: worker presence response missing channel_data", zap.String("id", client.ID), zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	if !ap.validateChannelSignature(client.ID, channel, response.Auth, response.ChannelData) {
		if ap.metrics != nil {
			ap.metrics.AuthFailures.WithLabelValues("invalid_signature").Inc()
		}
		ap.logger.Warn("Auth: worker response signature mismatch", zap.String("id", client.ID), zap.String("channel", channel))
		return AuthResult{Allowed: false}
	}

	return AuthResult{Allowed: true, UserData: body}
}

func (ap *WorkerAuthProvider) validateChannelSignature(socketID, channel, auth, channelData string) bool {
	parts := strings.Split(auth, ":")
	if len(parts) != 2 || parts[0] != ap.appKey {
		return false
	}
	return validSignature(ap.secret, parts[1], channelStringToSign(socketID, channel, channelData))
}

func channelStringToSign(socketID, channel, channelData string) string {
	if channelData == "" {
		return socketID + ":" + channel
	}
	return socketID + ":" + channel + ":" + channelData
}

func validSignature(secret, signature, stringToSign string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
