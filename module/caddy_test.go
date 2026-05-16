package websocket

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestWebsocketModuleDeliveryConfigDefaults(t *testing.T) {
	m := WebsocketModule{
		AppID:      "pogo-app",
		AuthPath:   "/pogo/auth",
		AuthScript: "/tmp/auth.php",
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
}

func TestWebsocketModuleParsesDeliveryConfig(t *testing.T) {
	d := caddyfile.NewTestDispenser(`pogo_websocket {
		app_id pogo-app
		auth_path /pogo/auth
		auth_script /tmp/auth.php
		outbound_queue_size 128
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
	if !m.EnableCompression {
		t.Fatal("EnableCompression = false, want true")
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
				auth_path /pogo/auth
				auth_script /tmp/auth.php
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
	t.Setenv("POGO_WS_WRITE_BURST_SIZE", "8")
	t.Setenv("POGO_WS_ENABLE_COMPRESSION", "true")

	m := WebsocketModule{
		AppID:             "pogo-app",
		AuthPath:          "/pogo/auth",
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
				AuthPath:   "/pogo/auth",
				AuthScript: "/tmp/auth.php",
			}

			if err := m.validateAndDefaults(); err == nil {
				t.Fatalf("validateAndDefaults accepted removed env override %s", name)
			}
		})
	}
}
