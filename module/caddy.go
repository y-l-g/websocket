package websocket

import (
	"context"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dunglas/frankenphp"
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
	AppSecret          string   `json:"app_secret,omitempty"`
	AuthPath           string   `json:"auth_path,omitempty"`
	AuthScript         string   `json:"auth_script,omitempty"`
	NumWorkers         int      `json:"num_workers,omitempty"`
	MaxConnections     int      `json:"max_connections,omitempty"`
	MaxAuthBody        int      `json:"max_auth_body,omitempty"`
	MaxConcurrentAuth  int      `json:"max_concurrent_auth,omitempty"`
	NumShards          int      `json:"num_shards,omitempty"`
	HandshakeRate      float64  `json:"handshake_rate,omitempty"`
	HandshakeBurst     int      `json:"handshake_burst,omitempty"`
	OutboundQueueSize  int      `json:"outbound_queue_size,omitempty"`
	BrokerQueueSize    int      `json:"broker_queue_size,omitempty"`
	ShardQueueSize     int      `json:"shard_queue_size,omitempty"`
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
	ShutdownTimeout    string   `json:"shutdown_timeout,omitempty"`

	PingPeriod string `json:"ping_period,omitempty"`
	WriteWait  string `json:"write_wait,omitempty"`
	PongWait   string `json:"pong_wait,omitempty"`

	pingPeriodDuration time.Duration
	writeWaitDuration  time.Duration
	pongWaitDuration   time.Duration
	shutdownTimeout    time.Duration

	hub              *Hub
	metrics          *Metrics
	workerHandle     frankenphp.Workers
	webhook          *WebhookManager
	logger           *zap.Logger
	upgrader         websocket.Upgrader
	allowedOriginSet map[string]struct{}
	limitersMu       sync.Mutex
	limiters         map[string]*remoteHandshakeLimiter
	ctx              context.Context
	cancel           context.CancelFunc
}

type remoteHandshakeLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

const (
	handshakeLimiterTTL  = 5 * time.Minute
	maxHandshakeLimiters = 4096
)

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
		m.AppID,
		m.AuthPath,
		m.MaxAuthBody,
		m.MaxConcurrentAuth,
		m.AppSecret,
	)

	m.webhook = NewWebhookManager(m.logger, m.WebhookURL, m.WebhookSecret, m.metrics)

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
		BrokerQueueSize:    m.BrokerQueueSize,
		ShardQueueSize:     m.ShardQueueSize,
		ShutdownTimeout:    m.shutdownTimeout,
	}
	m.hub = NewHub(m.AppID, m.logger, m.ctx, m.metrics, authProvider, m.webhook, broker, m.MaxConnections, m.NumShards, m.pingPeriodDuration, delivery)
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
		m.limiters = make(map[string]*remoteHandshakeLimiter)
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
