package websocket

import (
	"testing"
	"time"

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
	if m.FanoutBackpressureThreshold != DefaultFanoutBackpressureThreshold {
		t.Fatalf("FanoutBackpressureThreshold = %d, want %d", m.FanoutBackpressureThreshold, DefaultFanoutBackpressureThreshold)
	}
	if m.fanoutBackpressureMaxWaitDuration != DefaultFanoutBackpressureMaxWait {
		t.Fatalf("fanoutBackpressureMaxWaitDuration = %s, want %s", m.fanoutBackpressureMaxWaitDuration, DefaultFanoutBackpressureMaxWait)
	}
	if m.FanoutMode != DefaultFanoutMode {
		t.Fatalf("FanoutMode = %s, want %s", m.FanoutMode, DefaultFanoutMode)
	}
	if m.FanoutRoundSize != DefaultFanoutRoundSize {
		t.Fatalf("FanoutRoundSize = %d, want %d", m.FanoutRoundSize, DefaultFanoutRoundSize)
	}
	if m.fanoutRoundYieldDuration != DefaultFanoutRoundYield {
		t.Fatalf("fanoutRoundYieldDuration = %s, want %s", m.fanoutRoundYieldDuration, DefaultFanoutRoundYield)
	}
}

func TestWebsocketModuleParsesDeliveryConfig(t *testing.T) {
	d := caddyfile.NewTestDispenser(`pogo_websocket {
		app_id pogo-app
		auth_path /pogo/auth
		auth_script /tmp/auth.php
		outbound_queue_size 128
		write_burst_size 8
		fanout_backpressure_threshold 4
		fanout_backpressure_max_wait 5ms
		fanout_mode paced
		fanout_round_size 8
		fanout_round_yield 1ms
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
	if m.FanoutBackpressureThreshold != 4 {
		t.Fatalf("FanoutBackpressureThreshold = %d, want 4", m.FanoutBackpressureThreshold)
	}
	if m.fanoutBackpressureMaxWaitDuration != 5*time.Millisecond {
		t.Fatalf("fanoutBackpressureMaxWaitDuration = %s, want 5ms", m.fanoutBackpressureMaxWaitDuration)
	}
	if m.FanoutMode != fanoutModePaced {
		t.Fatalf("FanoutMode = %s, want paced", m.FanoutMode)
	}
	if m.FanoutRoundSize != 8 {
		t.Fatalf("FanoutRoundSize = %d, want 8", m.FanoutRoundSize)
	}
	if m.fanoutRoundYieldDuration != time.Millisecond {
		t.Fatalf("fanoutRoundYieldDuration = %s, want 1ms", m.fanoutRoundYieldDuration)
	}
	if !m.EnableCompression {
		t.Fatal("EnableCompression = false, want true")
	}
}

func TestWebsocketModuleDeliveryConfigEnvOverrides(t *testing.T) {
	t.Setenv("POGO_WS_OUTBOUND_QUEUE_SIZE", "512")
	t.Setenv("POGO_WS_WRITE_BURST_SIZE", "8")
	t.Setenv("POGO_WS_FANOUT_BACKPRESSURE_THRESHOLD", "4")
	t.Setenv("POGO_WS_FANOUT_BACKPRESSURE_MAX_WAIT", "5ms")
	t.Setenv("POGO_WS_FANOUT_MODE", "paced")
	t.Setenv("POGO_WS_FANOUT_ROUND_SIZE", "8")
	t.Setenv("POGO_WS_FANOUT_ROUND_YIELD", "1ms")
	t.Setenv("POGO_WS_ENABLE_COMPRESSION", "true")

	m := WebsocketModule{
		AppID:                       "pogo-app",
		AuthPath:                    "/pogo/auth",
		AuthScript:                  "/tmp/auth.php",
		OutboundQueueSize:           DefaultOutboundQueueSize,
		WriteBurstSize:              DefaultWriteBurstSize,
		FanoutBackpressureThreshold: DefaultFanoutBackpressureThreshold,
		FanoutBackpressureMaxWait:   DefaultFanoutBackpressureMaxWait.String(),
		FanoutMode:                  DefaultFanoutMode,
		FanoutRoundSize:             DefaultFanoutRoundSize,
		FanoutRoundYield:            DefaultFanoutRoundYield.String(),
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
	if m.FanoutBackpressureThreshold != 4 {
		t.Fatalf("FanoutBackpressureThreshold = %d, want 4", m.FanoutBackpressureThreshold)
	}
	if m.fanoutBackpressureMaxWaitDuration != 5*time.Millisecond {
		t.Fatalf("fanoutBackpressureMaxWaitDuration = %s, want 5ms", m.fanoutBackpressureMaxWaitDuration)
	}
	if m.FanoutMode != fanoutModePaced {
		t.Fatalf("FanoutMode = %s, want paced", m.FanoutMode)
	}
	if m.FanoutRoundSize != 8 {
		t.Fatalf("FanoutRoundSize = %d, want 8", m.FanoutRoundSize)
	}
	if m.fanoutRoundYieldDuration != time.Millisecond {
		t.Fatalf("fanoutRoundYieldDuration = %s, want 1ms", m.fanoutRoundYieldDuration)
	}
	if !m.EnableCompression {
		t.Fatal("EnableCompression = false, want true")
	}
}
