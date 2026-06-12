package websocket

import (
	"net/http"
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap"
)

func TestWebsocketModuleDeliveryConfigDefaults(t *testing.T) {
	m := WebsocketModule{
		AppID:      "pogo-app",
		AppKey:     "pogo-key",
		AuthPath:   "/broadcasting/auth",
		AuthScript: "/tmp/auth.php",
		AppSecret:  "test-secret",
	}

	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}

	if m.OutboundQueueSize != DefaultOutboundQueueSize {
		t.Fatalf("OutboundQueueSize = %d, want %d", m.OutboundQueueSize, DefaultOutboundQueueSize)
	}
	if m.WriteBurstSize != DefaultWriteBurstSize {
		t.Fatalf("WriteBurstSize = %d, want %d", m.WriteBurstSize, DefaultWriteBurstSize)
	}
	if m.BrokerQueueSize != DefaultBrokerQueueSize {
		t.Fatalf("BrokerQueueSize = %d, want %d", m.BrokerQueueSize, DefaultBrokerQueueSize)
	}
	if m.ShardQueueSize != DefaultShardQueueSize {
		t.Fatalf("ShardQueueSize = %d, want %d", m.ShardQueueSize, DefaultShardQueueSize)
	}
}

func TestWebsocketModuleParsesDeliveryConfig(t *testing.T) {
	d := caddyfile.NewTestDispenser(`pogo_websocket {
		app_id pogo-app
		app_key pogo-key
		auth_path /broadcasting/auth
		auth_script /tmp/auth.php
		app_secret test-secret
		outbound_queue_size 128
		broker_queue_size 256
		shard_queue_size 512
		write_burst_size 8
		enable_compression true
	}`)

	var m WebsocketModule
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}

	if m.OutboundQueueSize != 128 {
		t.Fatalf("OutboundQueueSize = %d, want 128", m.OutboundQueueSize)
	}
	if m.WriteBurstSize != 8 {
		t.Fatalf("WriteBurstSize = %d, want 8", m.WriteBurstSize)
	}
	if m.BrokerQueueSize != 256 {
		t.Fatalf("BrokerQueueSize = %d, want 256", m.BrokerQueueSize)
	}
	if m.ShardQueueSize != 512 {
		t.Fatalf("ShardQueueSize = %d, want 512", m.ShardQueueSize)
	}
	if !m.EnableCompression {
		t.Fatal("EnableCompression = false, want true")
	}
}

func TestWebsocketModuleRequiresAppSecret(t *testing.T) {
	m := WebsocketModule{
		AppID:      "pogo-app",
		AppKey:     "pogo-key",
		AuthPath:   "/broadcasting/auth",
		AuthScript: "/tmp/auth.php",
	}

	if err := m.validateAndDefaults(); err == nil {
		t.Fatal("validateAndDefaults accepted missing app_secret")
	}
}

func TestWebsocketModuleUsesCanonicalAppSecretEnv(t *testing.T) {
	t.Setenv("REVERB_APP_SECRET", "canonical-secret")

	m := WebsocketModule{
		AppID:      "pogo-app",
		AppKey:     "pogo-key",
		AuthPath:   "/broadcasting/auth",
		AuthScript: "/tmp/auth.php",
	}

	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}
	if m.AppSecret != "canonical-secret" {
		t.Fatalf("AppSecret = %q, want canonical-secret", m.AppSecret)
	}
}

func TestWebsocketModuleUsesReverbCredentialEnv(t *testing.T) {
	t.Setenv("REVERB_APP_ID", "env-app")
	t.Setenv("REVERB_APP_KEY", "env-key")
	t.Setenv("REVERB_APP_SECRET", "env-secret")

	m := WebsocketModule{
		AuthPath:   "/broadcasting/auth",
		AuthScript: "/tmp/auth.php",
	}

	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}
	if m.AppID != "env-app" {
		t.Fatalf("AppID = %q, want env-app", m.AppID)
	}
	if m.AppKey != "env-key" {
		t.Fatalf("AppKey = %q, want env-key", m.AppKey)
	}
	if m.AppSecret != "env-secret" {
		t.Fatalf("AppSecret = %q, want env-secret", m.AppSecret)
	}

	explicit := WebsocketModule{
		AppID:      "pogo-app",
		AppKey:     "pogo-key",
		AuthPath:   "/broadcasting/auth",
		AuthScript: "/tmp/auth.php",
		AppSecret:  "explicit-secret",
	}
	if err := explicit.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}
	if explicit.AppSecret != "explicit-secret" {
		t.Fatalf("explicit AppSecret = %q, want explicit-secret", explicit.AppSecret)
	}
}

func TestWebsocketModuleParsesShutdownTimeout(t *testing.T) {
	d := caddyfile.NewTestDispenser(`pogo_websocket {
		app_id pogo-app
		app_key pogo-key
		auth_path /broadcasting/auth
		auth_script /tmp/auth.php
		app_secret test-secret
		shutdown_timeout 250ms
	}`)

	var m WebsocketModule
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}
	if m.shutdownTimeout.String() != "250ms" {
		t.Fatalf("shutdownTimeout = %s, want 250ms", m.shutdownTimeout)
	}
}

func TestWebsocketModuleProtocolParsing(t *testing.T) {
	for _, proto := range []string{"5", "7", "10"} {
		if !isSupportedProtocol(proto) {
			t.Fatalf("Expected protocol %s to be supported", proto)
		}
	}
	for _, proto := range []string{"", "4", "abc"} {
		if isSupportedProtocol(proto) {
			t.Fatalf("Expected protocol %s to be rejected", proto)
		}
	}
}

func TestWebsocketModuleAppKeyFromPath(t *testing.T) {
	if got := appKeyFromPath("/app/pogo-app"); got != "pogo-app" {
		t.Fatalf("appKeyFromPath = %q, want pogo-app", got)
	}
	if got := appKeyFromPath("/app/pogo-app/extra"); got != "pogo-app" {
		t.Fatalf("appKeyFromPath = %q, want pogo-app", got)
	}
	if got := appKeyFromPath("/other/pogo-app"); got != "" {
		t.Fatalf("appKeyFromPath = %q, want empty", got)
	}
}

func TestWebsocketModuleParsesAllowedOrigins(t *testing.T) {
	d := caddyfile.NewTestDispenser(`pogo_websocket {
		app_id pogo-app
		app_key pogo-key
		auth_path /broadcasting/auth
		auth_script /tmp/auth.php
		app_secret test-secret
		allowed_origins https://example.com https://app.example.com:8443
	}`)

	var m WebsocketModule
	if err := m.UnmarshalCaddyfile(d); err != nil {
		t.Fatalf("UnmarshalCaddyfile returned error: %v", err)
	}
	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}

	if _, ok := m.allowedOriginSet["https://example.com"]; !ok {
		t.Fatal("Expected https://example.com in allowed origin set")
	}
	if _, ok := m.allowedOriginSet["https://app.example.com:8443"]; !ok {
		t.Fatal("Expected https://app.example.com:8443 in allowed origin set")
	}
}

func TestWebsocketModuleCheckOrigin(t *testing.T) {
	m := WebsocketModule{
		AppID:      "pogo-app",
		AppKey:     "pogo-key",
		AuthPath:   "/broadcasting/auth",
		AuthScript: "/tmp/auth.php",
		AppSecret:  "test-secret",
		logger:     zap.NewNop(),
	}
	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}

	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "missing origin", host: "example.com", want: true},
		{name: "same host", host: "example.com", origin: "https://example.com", want: true},
		{name: "same host with port", host: "example.com:8443", origin: "https://example.com:8443", want: true},
		{name: "cross host", host: "example.com", origin: "https://evil.test", want: false},
		{name: "malformed", host: "example.com", origin: "::::", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "https://"+tt.host+"/app/test", nil)
			if err != nil {
				t.Fatalf("NewRequest failed: %v", err)
			}
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			if got := m.checkOrigin(req); got != tt.want {
				t.Fatalf("checkOrigin = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebsocketModuleHandshakeLimiterIsPerRemoteAddr(t *testing.T) {
	m := WebsocketModule{
		HandshakeRate:  1,
		HandshakeBurst: 1,
		limiters:       make(map[string]*remoteHandshakeLimiter),
	}

	req1, err := http.NewRequest(http.MethodGet, "https://example.com/app/test", nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req1.RemoteAddr = "192.0.2.10:1234"

	req2, err := http.NewRequest(http.MethodGet, "https://example.com/app/test", nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req2.RemoteAddr = "192.0.2.11:1234"

	if !m.allowHandshake(req1) {
		t.Fatal("Expected first request from remote 1 to pass")
	}
	if m.allowHandshake(req1) {
		t.Fatal("Expected second immediate request from remote 1 to be limited")
	}
	if !m.allowHandshake(req2) {
		t.Fatal("Expected request from a different remote to pass")
	}
}

func TestWebsocketModuleCheckOriginAllowlist(t *testing.T) {
	m := WebsocketModule{
		AppID:          "pogo-app",
		AppKey:         "pogo-key",
		AuthPath:       "/broadcasting/auth",
		AuthScript:     "/tmp/auth.php",
		AppSecret:      "test-secret",
		logger:         zap.NewNop(),
		AllowedOrigins: []string{"https://app.example.com"},
	}
	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/app/test", nil)
	if err != nil {
		t.Fatalf("NewRequest failed: %v", err)
	}
	req.Host = "api.example.com"
	req.Header.Set("Origin", "https://app.example.com")
	if !m.checkOrigin(req) {
		t.Fatal("Expected configured origin to be accepted")
	}

	req.Header.Set("Origin", "https://evil.test")
	if m.checkOrigin(req) {
		t.Fatal("Expected unconfigured origin to be rejected")
	}
}

func TestWebsocketModuleRejectsRemovedFanoutDirectives(t *testing.T) {
	for _, directive := range []string{
		"fanout_backpressure_threshold 4",
		"fanout_backpressure_max_wait 5ms",
		"fanout_mode paced",
		"fanout_round_size 8",
		"fanout_round_yield 1ms",
	} {
		t.Run(directive, func(t *testing.T) {
			d := caddyfile.NewTestDispenser(`pogo_websocket {
				app_id pogo-app
				app_key pogo-key
				auth_path /broadcasting/auth
				auth_script /tmp/auth.php
				app_secret test-secret
				` + directive + `
			}`)

			var m WebsocketModule
			if err := m.UnmarshalCaddyfile(d); err == nil {
				t.Fatalf("UnmarshalCaddyfile accepted removed directive %q", directive)
			}
		})
	}
}

func TestWebsocketModuleDeliveryConfigEnvOverrides(t *testing.T) {
	t.Setenv("POGO_WS_OUTBOUND_QUEUE_SIZE", "512")
	t.Setenv("POGO_WS_BROKER_QUEUE_SIZE", "1024")
	t.Setenv("POGO_WS_SHARD_QUEUE_SIZE", "2048")
	t.Setenv("POGO_WS_WRITE_BURST_SIZE", "8")
	t.Setenv("POGO_WS_ENABLE_COMPRESSION", "true")
	t.Setenv("REVERB_APP_SECRET", "test-secret")

	m := WebsocketModule{
		AppID:             "pogo-app",
		AppKey:            "pogo-key",
		AuthPath:          "/broadcasting/auth",
		AuthScript:        "/tmp/auth.php",
		OutboundQueueSize: DefaultOutboundQueueSize,
		WriteBurstSize:    DefaultWriteBurstSize,
	}

	if err := m.validateAndDefaults(); err != nil {
		t.Fatalf("validateAndDefaults returned error: %v", err)
	}

	if m.OutboundQueueSize != 512 {
		t.Fatalf("OutboundQueueSize = %d, want 512", m.OutboundQueueSize)
	}
	if m.WriteBurstSize != 8 {
		t.Fatalf("WriteBurstSize = %d, want 8", m.WriteBurstSize)
	}
	if m.BrokerQueueSize != 1024 {
		t.Fatalf("BrokerQueueSize = %d, want 1024", m.BrokerQueueSize)
	}
	if m.ShardQueueSize != 2048 {
		t.Fatalf("ShardQueueSize = %d, want 2048", m.ShardQueueSize)
	}
	if !m.EnableCompression {
		t.Fatal("EnableCompression = false, want true")
	}
}

func TestWebsocketModuleRejectsRemovedFanoutEnvOverrides(t *testing.T) {
	for _, name := range []string{
		"POGO_WS_FANOUT_BACKPRESSURE_THRESHOLD",
		"POGO_WS_FANOUT_BACKPRESSURE_MAX_WAIT",
		"POGO_WS_FANOUT_MODE",
		"POGO_WS_FANOUT_ROUND_SIZE",
		"POGO_WS_FANOUT_ROUND_YIELD",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "removed")

			m := WebsocketModule{
				AppID:      "pogo-app",
				AppKey:     "pogo-key",
				AuthPath:   "/broadcasting/auth",
				AuthScript: "/tmp/auth.php",
				AppSecret:  "test-secret",
			}

			if err := m.validateAndDefaults(); err == nil {
				t.Fatalf("validateAndDefaults accepted removed env override %s", name)
			}
		})
	}
}
