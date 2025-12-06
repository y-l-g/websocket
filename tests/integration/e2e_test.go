package integration

import (
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	ServerPort = "9090"
	WsURL      = "ws://127.0.0.1:" + ServerPort + "/app/test-app"
	HttpURL    = "http://127.0.0.1:" + ServerPort
)

func TestEndToEnd(t *testing.T) {
	// --- SETUP ---
	rootDir, _ := filepath.Abs("../../")
	binPath := filepath.Join(rootDir, "frankenphp")

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Fatalf("frankenphp binary not found at %s. Run 'make build' first.", binPath)
	}

	cmd := exec.Command(binPath, "run", "--config", "tests/integration/fixtures/Caddyfile")
	cmd.Dir = rootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	if !waitForServer() {
		t.Fatal("Server failed to start on port " + ServerPort)
	}

	// --- TEST SCENARIOS ---

	t.Run("HappyPath_PrivateChannel", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(WsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()

		expectHandshake(t, ws)

		// Subscribe
		subPayload := `{"event":"pusher:subscribe","data":{"channel":"private-test"}}`
		if err := ws.WriteMessage(websocket.TextMessage, []byte(subPayload)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Expect Success
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if msg["event"] != "pusher_internal:subscription_succeeded" {
			t.Errorf("Expected subscription success, got: %v", msg)
		}

		// Trigger Publish
		params := url.Values{}
		params.Add("app_id", "test-app")
		params.Add("channel", "private-test")
		params.Add("event", "my-event")
		params.Add("data", `{"foo":"bar"}`)

		pubUrl := HttpURL + "/publish/publish.php?" + params.Encode()
		resp, err := http.Get(pubUrl)
		if err != nil {
			t.Fatalf("Publish request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != 200 {
			t.Errorf("Publish endpoint returned %d", resp.StatusCode)
		}

		// Expect Broadcast
		if err := ws.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline failed: %v", err)
		}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Broadcast read failed: %v", err)
		}
		if msg["event"] != "my-event" {
			t.Errorf("Expected 'my-event', got %v", msg["event"])
		}
	})

	t.Run("Failure_AuthRejection", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(WsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()

		expectHandshake(t, ws)

		// Subscribe to FORBIDDEN channel
		subPayload := `{"event":"pusher:subscribe","data":{"channel":"private-forbidden"}}`
		if err := ws.WriteMessage(websocket.TextMessage, []byte(subPayload)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Expect Error
		var msg map[string]interface{}
		if err := ws.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline failed: %v", err)
		}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		// Pusher Protocol v7: Auth error sends pusher:error
		if msg["event"] != "pusher:error" {
			t.Errorf("Expected pusher:error, got: %v", msg)
		}

		// Verify Error Code (4009 = Subscription Denied)
		data, _ := msg["data"].(map[string]interface{})
		if code, ok := data["code"].(float64); !ok || int(code) != 4009 {
			t.Errorf("Expected error code 4009, got %v", data["code"])
		}
	})

	t.Run("Robustness_InvalidJSON", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(WsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()
		expectHandshake(t, ws)

		// Send Garbage
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{INVALID_JSON`)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Server should NOT disconnect immediately (log warning internally).
		// We verify it's still alive by sending a valid Ping.
		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"event":"pusher:ping"}`)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Expect Pong
		var msg map[string]interface{}
		if err := ws.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline failed: %v", err)
		}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Server died or didn't respond to ping after garbage: %v", err)
		}
		if msg["event"] != "pusher:pong" {
			t.Errorf("Expected pong, got %v", msg)
		}
	})
}

func expectHandshake(t *testing.T, ws *websocket.Conn) {
	var connectMsg map[string]interface{}
	if err := ws.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline failed: %v", err)
	}
	if err := ws.ReadJSON(&connectMsg); err != nil {
		t.Fatalf("Failed to read handshake: %v", err)
	}
	if connectMsg["event"] != "pusher:connection_established" {
		t.Fatalf("Unexpected handshake: %v", connectMsg)
	}
}

func waitForServer() bool {
	for i := 0; i < 20; i++ {
		conn, err := http.Get(HttpURL)
		if err == nil {
			_ = conn.Body.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
