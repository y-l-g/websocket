package websocket

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

func (m *WebsocketModule) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	normalized, ok := normalizeOrigin(origin)
	if !ok {
		m.logger.Warn("WebSocket origin rejected: malformed origin", zap.String("origin", origin))
		return false
	}

	if len(m.allowedOriginSet) > 0 {
		if _, ok := m.allowedOriginSet[normalized]; ok {
			return true
		}
		m.logger.Warn("WebSocket origin rejected: not in allowlist", zap.String("origin", origin), zap.String("host", r.Host))
		return false
	}

	parsedOrigin, _ := url.Parse(origin)
	if strings.EqualFold(parsedOrigin.Host, r.Host) {
		return true
	}

	m.logger.Warn("WebSocket origin rejected: host mismatch", zap.String("origin", origin), zap.String("host", r.Host))
	return false
}

func (m *WebsocketModule) allowHandshake(r *http.Request) bool {
	if m.HandshakeRate < 0 {
		return true
	}
	if m.limiters == nil {
		return true
	}

	key := remoteAddrKey(r.RemoteAddr)
	now := time.Now()

	m.limitersMu.Lock()
	defer m.limitersMu.Unlock()

	if len(m.limiters) >= maxHandshakeLimiters {
		m.evictStaleHandshakeLimiters(now)
	}
	if len(m.limiters) >= maxHandshakeLimiters {
		for existing := range m.limiters {
			delete(m.limiters, existing)
			break
		}
	}

	remoteLimiter := m.limiters[key]
	if remoteLimiter == nil {
		remoteLimiter = &remoteHandshakeLimiter{
			limiter: rate.NewLimiter(rate.Limit(m.HandshakeRate), m.HandshakeBurst),
		}
		m.limiters[key] = remoteLimiter
	}
	remoteLimiter.lastSeen = now
	return remoteLimiter.limiter.Allow()
}

func (m *WebsocketModule) evictStaleHandshakeLimiters(now time.Time) {
	for key, limiter := range m.limiters {
		if now.Sub(limiter.lastSeen) > handshakeLimiterTTL {
			delete(m.limiters, key)
		}
	}
}

func remoteAddrKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	return remoteAddr
}

func (m *WebsocketModule) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.URL.Path == "/pogo/health" {
		m.serveHealth(w)
		return nil
	}

	if proto := r.URL.Query().Get("protocol"); proto != "" {
		if !isSupportedProtocol(proto) {
			http.Error(w, "Unsupported protocol version", http.StatusBadRequest)
			return nil
		}
	}

	if !websocket.IsWebSocketUpgrade(r) {
		return next.ServeHTTP(w, r)
	}

	if key := appKeyFromPath(r.URL.Path); key != m.AppID {
		http.Error(w, "Invalid app key", http.StatusForbidden)
		return nil
	}

	if !m.allowHandshake(r) {
		m.logger.Warn("Handshake rate limit exceeded", zap.String("remote_addr", r.RemoteAddr))
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return nil
	}

	headers := r.Header.Clone()

	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		m.logger.Error("Upgrade failed", zap.Error(err))
		return err
	}
	conn.EnableWriteCompression(m.EnableCompression)

	nano := time.Now().UnixNano()
	clientID := fmt.Sprintf("%d.%d", nano/1e9, nano%1e9)

	ctx, cancel := context.WithCancel(r.Context())

	client := &Client{
		ID:             clientID,
		hub:            m.hub,
		conn:           conn,
		send:           make(chan any, m.OutboundQueueSize),
		Headers:        headers,
		ctx:            ctx,
		cancel:         cancel,
		PingPeriod:     m.pingPeriodDuration,
		WriteWait:      m.writeWaitDuration,
		PongWait:       m.pongWaitDuration,
		WriteBurstSize: m.WriteBurstSize,
		msgLimiter:     rate.NewLimiter(rate.Limit(m.ClientMsgRateLimit), m.ClientMsgRateBurst),
	}

	if !m.hub.Register(client) {
		cancel()
		return nil
	}

	go client.writePump()
	client.readPump()

	return nil
}

func isSupportedProtocol(raw string) bool {
	version, err := strconv.Atoi(raw)
	return err == nil && version >= 5
}

func appKeyFromPath(path string) string {
	key := strings.TrimPrefix(path, "/app/")
	if key == path {
		return ""
	}
	if idx := strings.IndexByte(key, '/'); idx >= 0 {
		key = key[:idx]
	}
	return key
}

func (m *WebsocketModule) serveHealth(w http.ResponseWriter) {
	status := "ok"
	code := http.StatusOK

	if m.hub == nil {
		status = "hub_not_initialized"
		code = http.StatusServiceUnavailable
	} else if !m.hub.IsHealthy() {
		status = m.hub.HealthError()
		if status == "" {
			status = "hub_unhealthy"
		}
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
}
