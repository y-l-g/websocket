package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dunglas/frankenphp"
	frankenphpCaddy "github.com/dunglas/frankenphp/caddy"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

func init() {
	caddy.RegisterModule(WebsocketModule{})
	httpcaddyfile.RegisterHandlerDirective("pogo_websocket", parseCaddyfile)
}

type WebsocketModule struct {
	AppID              string   `json:"app_id,omitempty"`
	AuthPath           string   `json:"auth_path,omitempty"`
	AuthScript         string   `json:"auth_script,omitempty"`
	NumWorkers         int      `json:"num_workers,omitempty"`
	MaxConnections     int      `json:"max_connections,omitempty"`
	MaxAuthBody        int      `json:"max_auth_body,omitempty"`
	MaxConcurrentAuth  int      `json:"max_concurrent_auth,omitempty"`
	NumShards          int      `json:"num_shards,omitempty"`
	HandshakeRate      float64  `json:"handshake_rate,omitempty"`  // New
	HandshakeBurst     int      `json:"handshake_burst,omitempty"` // New
	OutboundQueueSize  int      `json:"outbound_queue_size,omitempty"`
	WriteBurstSize     int      `json:"write_burst_size,omitempty"`
	ClientMsgRateLimit float64  `json:"client_msg_rate_limit,omitempty"`
	ClientMsgRateBurst int      `json:"client_msg_rate_burst,omitempty"`
	EnableCompression  bool     `json:"enable_compression,omitempty"`
	AllowedOrigins     []string `json:"allowed_origins,omitempty"`
	WebhookURL         string   `json:"webhook_url,omitempty"`
	WebhookSecret      string   `json:"webhook_secret,omitempty"`
	RedisHost          string   `json:"redis_host,omitempty"`
	RedisPassword      string   `json:"redis_password,omitempty"`
	RedisDB            int      `json:"redis_db,omitempty"`
	RedisTLS           bool     `json:"redis_tls,omitempty"`

	// Timeouts
	PingPeriod string `json:"ping_period,omitempty"`
	WriteWait  string `json:"write_wait,omitempty"`
	PongWait   string `json:"pong_wait,omitempty"`

	// Parsed Durations
	pingPeriodDuration time.Duration
	writeWaitDuration  time.Duration
	pongWaitDuration   time.Duration

	hub              *Hub
	metrics          *Metrics
	workerHandle     frankenphp.Workers
	limiter          *rate.Limiter // Rate Limiter
	logger           *zap.Logger
	upgrader         websocket.Upgrader
	allowedOriginSet map[string]struct{}
	ctx              context.Context
	cancel           context.CancelFunc
}

func (WebsocketModule) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.pogo_websocket",
		New: func() caddy.Module { return new(WebsocketModule) },
	}
}

func (m *WebsocketModule) Provision(ctx caddy.Context) error {
	m.logger = ctx.Logger(m)
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.metrics = NewMetrics(ctx.GetMetricsRegistry())

	if err := m.validateAndDefaults(); err != nil {
		return err
	}

	if err := m.setupWorkers(); err != nil {
		return err
	}

	authProvider := NewWorkerAuthProvider(
		m.logger,
		m.metrics,
		m.workerHandle,
		m.AuthPath,
		m.MaxAuthBody,
		m.MaxConcurrentAuth,
		m.WebhookSecret,
	)

	webhook := NewWebhookManager(m.logger, m.WebhookURL, m.WebhookSecret)

	broker, err := m.setupBroker()
	if err != nil {
		return err
	}

	delivery := DeliveryConfig{
		OutboundQueueSize:  m.OutboundQueueSize,
		WriteBurstSize:     m.WriteBurstSize,
		ClientMsgRateLimit: m.ClientMsgRateLimit,
		ClientMsgRateBurst: m.ClientMsgRateBurst,
		EnableCompression:  m.EnableCompression,
	}
	m.hub = NewHub(m.AppID, m.logger, m.ctx, m.metrics, authProvider, webhook, broker, m.MaxConnections, m.NumShards, m.pingPeriodDuration, delivery)
	m.metrics.SetDeliveryConfig(delivery.withDefaults())

	if err := RegisterHub(m.AppID, m.hub); err != nil {
		return err
	}

	go m.hub.Run()

	m.upgrader = websocket.Upgrader{
		ReadBufferSize:    1024,
		WriteBufferSize:   1024,
		CheckOrigin:       m.checkOrigin,
		EnableCompression: m.EnableCompression,
	}

	if m.HandshakeRate >= 0 {
		if m.HandshakeRate == 0 {
			m.HandshakeRate = 100
		}
		if m.HandshakeBurst == 0 {
			m.HandshakeBurst = 50
		}
		m.limiter = rate.NewLimiter(rate.Limit(m.HandshakeRate), m.HandshakeBurst)
	}

	return nil
}

func (m *WebsocketModule) validateAndDefaults() error {
	if m.AppID == "" {
		return fmt.Errorf("the 'app_id' directive is required")
	}
	if m.AuthScript == "" {
		return fmt.Errorf("the 'auth_script' directive is required")
	}
	if m.AuthPath == "" {
		return fmt.Errorf("the 'auth_path' directive is required")
	}

	if m.NumWorkers == 0 {
		m.NumWorkers = 2
	}
	if m.MaxConnections == 0 {
		m.MaxConnections = 10000
	}
	if m.MaxAuthBody == 0 {
		m.MaxAuthBody = 16 * 1024
	}
	if m.MaxConcurrentAuth == 0 {
		m.MaxConcurrentAuth = 100
	}

	if m.NumShards == 0 {
		m.NumShards = runtime.NumCPU() * 2
		if m.NumShards > 64 {
			m.NumShards = 64
		}
		if m.NumShards < 4 {
			m.NumShards = 4
		}
	}

	defaultDelivery := DefaultDeliveryConfig()
	if err := m.applyDeliveryEnvOverrides(); err != nil {
		return err
	}
	if m.OutboundQueueSize == 0 {
		m.OutboundQueueSize = defaultDelivery.OutboundQueueSize
	}
	if m.OutboundQueueSize < 1 {
		return fmt.Errorf("outbound_queue_size must be greater than 0")
	}
	if m.WriteBurstSize == 0 {
		m.WriteBurstSize = defaultDelivery.WriteBurstSize
	}
	if m.WriteBurstSize < 1 {
		return fmt.Errorf("write_burst_size must be greater than 0")
	}
	if m.ClientMsgRateLimit == 0 {
		m.ClientMsgRateLimit = DefaultClientMsgRateLimit
	}
	if m.ClientMsgRateLimit < 0 {
		return fmt.Errorf("client_msg_rate_limit must not be negative")
	}
	if m.ClientMsgRateBurst == 0 {
		m.ClientMsgRateBurst = DefaultClientMsgRateBurst
	}
	if m.ClientMsgRateBurst < 1 {
		return fmt.Errorf("client_msg_rate_burst must be greater than 0")
	}

	m.allowedOriginSet = make(map[string]struct{}, len(m.AllowedOrigins))
	for _, origin := range m.AllowedOrigins {
		normalized, ok := normalizeOrigin(origin)
		if !ok {
			return fmt.Errorf("invalid allowed_origin %q", origin)
		}
		m.allowedOriginSet[normalized] = struct{}{}
	}

	var err error
	if m.PingPeriod == "" {
		m.pingPeriodDuration = DefaultPingPeriod
	} else {
		m.pingPeriodDuration, err = time.ParseDuration(m.PingPeriod)
		if err != nil {
			return fmt.Errorf("invalid ping_period: %v", err)
		}
	}

	if m.WriteWait == "" {
		m.writeWaitDuration = DefaultWriteWait
	} else {
		m.writeWaitDuration, err = time.ParseDuration(m.WriteWait)
		if err != nil {
			return fmt.Errorf("invalid write_wait: %v", err)
		}
	}

	if m.PongWait == "" {
		m.pongWaitDuration = DefaultPongWait
	} else {
		m.pongWaitDuration, err = time.ParseDuration(m.PongWait)
		if err != nil {
			return fmt.Errorf("invalid pong_wait: %v", err)
		}
	}

	return nil
}

func normalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}

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

func (m *WebsocketModule) applyDeliveryEnvOverrides() error {
	if value := os.Getenv("POGO_WS_OUTBOUND_QUEUE_SIZE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_OUTBOUND_QUEUE_SIZE: %v", err)
		}
		m.OutboundQueueSize = parsed
	}
	if value := os.Getenv("POGO_WS_WRITE_BURST_SIZE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_WRITE_BURST_SIZE: %v", err)
		}
		m.WriteBurstSize = parsed
	}
	if value := os.Getenv("POGO_WS_FANOUT_BACKPRESSURE_THRESHOLD"); value != "" {
		return fmt.Errorf("POGO_WS_FANOUT_BACKPRESSURE_THRESHOLD has been removed")
	}
	if value := os.Getenv("POGO_WS_FANOUT_BACKPRESSURE_MAX_WAIT"); value != "" {
		return fmt.Errorf("POGO_WS_FANOUT_BACKPRESSURE_MAX_WAIT has been removed")
	}
	if value := os.Getenv("POGO_WS_FANOUT_MODE"); value != "" {
		return fmt.Errorf("POGO_WS_FANOUT_MODE has been removed")
	}
	if value := os.Getenv("POGO_WS_FANOUT_ROUND_SIZE"); value != "" {
		return fmt.Errorf("POGO_WS_FANOUT_ROUND_SIZE has been removed")
	}
	if value := os.Getenv("POGO_WS_FANOUT_ROUND_YIELD"); value != "" {
		return fmt.Errorf("POGO_WS_FANOUT_ROUND_YIELD has been removed")
	}
	if value := os.Getenv("POGO_WS_ENABLE_COMPRESSION"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_ENABLE_COMPRESSION: %v", err)
		}
		m.EnableCompression = parsed
	}
	if value := os.Getenv("POGO_WS_CLIENT_MSG_RATE_LIMIT"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_CLIENT_MSG_RATE_LIMIT: %v", err)
		}
		m.ClientMsgRateLimit = parsed
	}
	if value := os.Getenv("POGO_WS_CLIENT_MSG_RATE_BURST"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_CLIENT_MSG_RATE_BURST: %v", err)
		}
		m.ClientMsgRateBurst = parsed
	}
	return nil
}

func (m *WebsocketModule) setupWorkers() error {
	m.workerHandle = frankenphpCaddy.RegisterWorkers(
		"frankenphp-websocket-auth-"+m.AppID,
		m.AuthScript,
		m.NumWorkers,
	)
	return nil
}

func (m *WebsocketModule) setupBroker() (Broker, error) {
	if m.RedisHost != "" {
		m.logger.Info("Using Redis Broker", zap.String("host", m.RedisHost), zap.Int("db", m.RedisDB), zap.Bool("tls", m.RedisTLS))
		return NewRedisBroker(m.logger, m.RedisHost, m.RedisPassword, m.RedisDB, m.RedisTLS), nil
	}
	m.logger.Info("Using Memory Broker")
	return NewMemoryBroker(m.logger, m.metrics), nil
}

func (m *WebsocketModule) Cleanup() error {
	m.cancel()
	if m.hub != nil {
		m.hub.Wait()
		UnregisterHub(m.AppID, m.hub)
	}
	return nil
}

func (m *WebsocketModule) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if r.URL.Path == "/pogo/health" {
		m.serveHealth(w)
		return nil
	}

	if proto := r.URL.Query().Get("protocol"); proto != "" {
		if proto < "5" {
			http.Error(w, "Unsupported protocol version", http.StatusBadRequest)
			return nil
		}
	}

	if !websocket.IsWebSocketUpgrade(r) {
		return next.ServeHTTP(w, r)
	}

	// Security: Rate Limit Handshakes
	if m.limiter != nil && !m.limiter.Allow() {
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

	m.hub.Register(client)

	go client.writePump()
	client.readPump()

	return nil
}

func (m *WebsocketModule) serveHealth(w http.ResponseWriter) {
	status := "ok"
	code := http.StatusOK

	if m.hub == nil {
		status = "hub_not_initialized"
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"status":"` + status + `"}`))
}

func (m *WebsocketModule) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "app_id":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.AppID = d.Val()
			case "auth_path":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.AuthPath = d.Val()
			case "auth_script":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.AuthScript = d.Val()
			case "num_workers":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var w int
				if _, err := fmt.Sscanf(d.Val(), "%d", &w); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.NumWorkers = w
			case "max_connections":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.MaxConnections = c
			case "max_auth_body":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var b int
				if _, err := fmt.Sscanf(d.Val(), "%d", &b); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.MaxAuthBody = b
			case "max_concurrent_auth":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.MaxConcurrentAuth = c
			case "num_shards":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.NumShards = c
			case "handshake_rate":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var r float64
				if _, err := fmt.Sscanf(d.Val(), "%f", &r); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.HandshakeRate = r
			case "handshake_burst":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.HandshakeBurst = c
			case "outbound_queue_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.OutboundQueueSize = c
			case "write_burst_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.WriteBurstSize = c
			case "fanout_backpressure_threshold":
				return d.Err("fanout_backpressure_threshold has been removed")
			case "fanout_backpressure_max_wait":
				return d.Err("fanout_backpressure_max_wait has been removed")
			case "fanout_mode":
				return d.Err("fanout_mode has been removed")
			case "fanout_round_size":
				return d.Err("fanout_round_size has been removed")
			case "fanout_round_yield":
				return d.Err("fanout_round_yield has been removed")
			case "enable_compression":
				if !d.NextArg() {
					m.EnableCompression = true
					continue
				}
				var enabled bool
				if _, err := fmt.Sscanf(d.Val(), "%t", &enabled); err != nil {
					return d.Errf("invalid boolean: %v", err)
				}
				m.EnableCompression = enabled
			case "allowed_origins":
				args := d.RemainingArgs()
				if len(args) == 0 {
					return d.ArgErr()
				}
				m.AllowedOrigins = append(m.AllowedOrigins, args...)
			case "client_msg_rate_limit":
				if !d.NextArg() {
					return d.ArgErr()
				}
				if _, err := fmt.Sscanf(d.Val(), "%f", &m.ClientMsgRateLimit); err != nil {
					return d.Errf("invalid number: %v", err)
				}
			case "client_msg_rate_burst":
				if !d.NextArg() {
					return d.ArgErr()
				}
				if _, err := fmt.Sscanf(d.Val(), "%d", &m.ClientMsgRateBurst); err != nil {
					return d.Errf("invalid number: %v", err)
				}
			case "ping_period":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.PingPeriod = d.Val()
			case "write_wait":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.WriteWait = d.Val()
			case "pong_wait":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.PongWait = d.Val()
			case "webhook_url":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.WebhookURL = d.Val()
			case "webhook_secret":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.WebhookSecret = d.Val()
			case "redis_host":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.RedisHost = d.Val()
			case "redis_password":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.RedisPassword = d.Val()
			case "redis_db":
				if !d.NextArg() {
					return d.ArgErr()
				}
				if _, err := fmt.Sscanf(d.Val(), "%d", &m.RedisDB); err != nil {
					return d.Errf("invalid number: %v", err)
				}
			case "redis_tls":
				m.RedisTLS = true
				if d.NextArg() {
					if _, err := fmt.Sscanf(d.Val(), "%t", &m.RedisTLS); err != nil {
						return d.Errf("invalid boolean: %v", err)
					}
				}
			}
		}
	}
	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m WebsocketModule
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return &m, err
}

var (
	_ caddy.Module                = (*WebsocketModule)(nil)
	_ caddy.Provisioner           = (*WebsocketModule)(nil)
	_ caddy.CleanerUpper          = (*WebsocketModule)(nil)
	_ caddyhttp.MiddlewareHandler = (*WebsocketModule)(nil)
	_ caddyfile.Unmarshaler       = (*WebsocketModule)(nil)
)
