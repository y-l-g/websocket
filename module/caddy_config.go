package websocket

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func (m *WebsocketModule) validateAndDefaults() error {
	if m.AppID == "" {
		m.AppID = os.Getenv("REVERB_APP_ID")
	}
	if m.AppID == "" {
		return fmt.Errorf("the 'app_id' directive or REVERB_APP_ID environment variable is required")
	}
	if m.AppKey == "" {
		m.AppKey = os.Getenv("REVERB_APP_KEY")
	}
	if m.AppKey == "" {
		return fmt.Errorf("the 'app_key' directive or REVERB_APP_KEY environment variable is required")
	}
	if m.AuthScript != "" && m.AuthPath == "" {
		m.AuthPath = "/broadcasting/auth"
	}
	if m.AuthScript == "" && m.AuthPath != "" {
		return fmt.Errorf("the 'auth_script' directive is required when auth_path is configured")
	}
	if m.AppSecret == "" {
		m.AppSecret = appSecretFromEnv()
	}
	if m.AppSecret == "" {
		return fmt.Errorf("the 'app_secret' directive is required")
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
	if m.BrokerQueueSize == 0 {
		m.BrokerQueueSize = defaultDelivery.BrokerQueueSize
	}
	if m.BrokerQueueSize < 1 {
		return fmt.Errorf("broker_queue_size must be greater than 0")
	}
	if m.ShardQueueSize == 0 {
		m.ShardQueueSize = defaultDelivery.ShardQueueSize
	}
	if m.ShardQueueSize < 1 {
		return fmt.Errorf("shard_queue_size must be greater than 0")
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

	if m.ShutdownTimeout == "" {
		m.shutdownTimeout = DefaultShutdownTimeout
	} else {
		m.shutdownTimeout, err = time.ParseDuration(m.ShutdownTimeout)
		if err != nil {
			return fmt.Errorf("invalid shutdown_timeout: %v", err)
		}
		if m.shutdownTimeout <= 0 {
			return fmt.Errorf("shutdown_timeout must be greater than 0")
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

func (m *WebsocketModule) applyDeliveryEnvOverrides() error {
	if value := os.Getenv("POGO_WS_OUTBOUND_QUEUE_SIZE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_OUTBOUND_QUEUE_SIZE: %v", err)
		}
		m.OutboundQueueSize = parsed
	}
	if value := os.Getenv("POGO_WS_BROKER_QUEUE_SIZE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_BROKER_QUEUE_SIZE: %v", err)
		}
		m.BrokerQueueSize = parsed
	}
	if value := os.Getenv("POGO_WS_SHARD_QUEUE_SIZE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid POGO_WS_SHARD_QUEUE_SIZE: %v", err)
		}
		m.ShardQueueSize = parsed
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

func appSecretFromEnv() string {
	return os.Getenv("REVERB_APP_SECRET")
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
			case "app_key":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.AppKey = d.Val()
			case "app_secret":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.AppSecret = d.Val()
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
			case "broker_queue_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.BrokerQueueSize = c
			case "shard_queue_size":
				if !d.NextArg() {
					return d.ArgErr()
				}
				var c int
				if _, err := fmt.Sscanf(d.Val(), "%d", &c); err != nil {
					return d.Errf("invalid number: %v", err)
				}
				m.ShardQueueSize = c
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
			case "shutdown_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.ShutdownTimeout = d.Val()
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
