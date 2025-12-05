package websocket

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dunglas/frankenphp"
	frankenphpCaddy "github.com/dunglas/frankenphp/caddy"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(WebsocketModule{})
	httpcaddyfile.RegisterHandlerDirective("pogo_websocket", parseCaddyfile)
}

type WebsocketModule struct {
	AppID         string `json:"app_id,omitempty"`
	AuthPath      string `json:"auth_path,omitempty"`
	AuthScript    string `json:"auth_script,omitempty"`
	NumWorkers    int    `json:"num_workers,omitempty"`
	WebhookURL    string `json:"webhook_url,omitempty"`
	WebhookSecret string `json:"webhook_secret,omitempty"`
	RedisHost     string `json:"redis_host,omitempty"`

	hub          *Hub
	metrics      *Metrics
	workerHandle frankenphp.Workers
	logger       *zap.Logger
	upgrader     websocket.Upgrader
	ctx          context.Context
	cancel       context.CancelFunc
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

	// --- CONFIGURATION STRICTE ---
	// Plus de magie "Auto-discovery". L'utilisateur doit savoir ce qu'il fait.

	if m.AppID == "" {
		return fmt.Errorf("the 'app_id' directive is required")
	}

	if m.AuthScript == "" {
		return fmt.Errorf("the 'auth_script' directive is required (path to your PHP worker script)")
	}

	if m.AuthPath == "" {
		return fmt.Errorf("the 'auth_path' directive is required (internal route for authentication)")
	}

	if m.NumWorkers == 0 {
		m.NumWorkers = 2
	}

	// Enregistrement des Workers FrankenPHP
	m.workerHandle = frankenphpCaddy.RegisterWorkers(
		"frankenphp-websocket-auth-"+m.AppID,
		m.AuthScript,
		m.NumWorkers,
	)

	authProvider := NewWorkerAuthProvider(m.logger, m.metrics, m.workerHandle, m.AuthPath)
	webhook := NewWebhookManager(m.logger, m.WebhookURL, m.WebhookSecret)

	var broker Broker
	if m.RedisHost != "" {
		m.logger.Info("Using Redis Broker", zap.String("host", m.RedisHost))
		broker = NewRedisBroker(m.logger, m.RedisHost)
	} else {
		m.logger.Info("Using Memory Broker")
		broker = NewMemoryBroker()
	}

	m.hub = NewHub(m.AppID, m.logger, m.ctx, m.metrics, authProvider, webhook, broker)

	RegisterHub(m.AppID, m.hub)

	go m.hub.Run()

	m.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	return nil
}

func (m *WebsocketModule) Cleanup() error {
	m.cancel()
	if m.hub != nil {
		m.hub.Wait()
		UnregisterHub(m.AppID)
	}
	return nil
}

func (m *WebsocketModule) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if !websocket.IsWebSocketUpgrade(r) {
		return next.ServeHTTP(w, r)
	}

	headers := r.Header.Clone()

	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		m.logger.Error("Upgrade failed", zap.Error(err))
		return err
	}

	nano := time.Now().UnixNano()
	clientID := fmt.Sprintf("%d.%d", nano/1e9, nano%1e9)

	client := &Client{
		ID:      clientID,
		hub:     m.hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		Headers: headers,
	}

	m.hub.Register(client)

	go client.writePump()
	client.readPump()

	return nil
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
