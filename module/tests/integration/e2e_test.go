package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestEndToEnd(t *testing.T) {
	rootDir, _ := filepath.Abs("../../")
	binPath := os.Getenv("FRANKENPHP_BINARY")
	if binPath == "" {
		binPath = filepath.Join(rootDir, "frankenphp")
	}

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skipf("Skipping E2E test: binary not found at %s", binPath)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen on ephemeral port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	caddyfileContent, err := os.ReadFile("fixtures/Caddyfile")
	if err != nil {
		t.Fatalf("Failed to read fixture Caddyfile: %v", err)
	}

	newCaddyfile := strings.ReplaceAll(string(caddyfileContent), ":9090", fmt.Sprintf(":%d", port))

	tmpCaddyfile, err := os.CreateTemp("", "Caddyfile.*")
	if err != nil {
		t.Fatalf("Failed to create temp Caddyfile: %v", err)
	}
	defer func() { _ = os.Remove(tmpCaddyfile.Name()) }()

	if _, err := tmpCaddyfile.WriteString(newCaddyfile); err != nil {
		t.Fatalf("Failed to write temp Caddyfile: %v", err)
	}
	_ = tmpCaddyfile.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "run", "--config", tmpCaddyfile.Name())
	cmd.Dir = rootDir

	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/app/test-app", port)

	if !waitForServer(baseURL) {
		t.Fatalf("Server failed to start on port %d within timeout", port)
	}

	t.Run("HappyPath_PrivateChannel", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()

		expectHandshake(t, ws)

		subPayload := `{"event":"pusher:subscribe","data":{"channel":"private-test"}}`
		if err := ws.WriteMessage(websocket.TextMessage, []byte(subPayload)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Read failed: %v", err)
		}
		if msg["event"] != "pusher_internal:subscription_succeeded" {
			t.Errorf("Expected subscription success, got: %v", msg)
		}

		params := url.Values{}
		params.Add("app_id", "test-app")
		params.Add("channel", "private-test")
		params.Add("event", "my-event")
		params.Add("data", `{"foo":"bar"}`)

		pubUrl := baseURL + "/publish/publish.php?" + params.Encode()
		resp, err := http.Get(pubUrl)
		if err != nil {
			t.Fatalf("Publish request failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != 200 {
			t.Errorf("Publish endpoint returned %d", resp.StatusCode)
		}

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
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()

		expectHandshake(t, ws)

		subPayload := `{"event":"pusher:subscribe","data":{"channel":"private-forbidden"}}`
		if err := ws.WriteMessage(websocket.TextMessage, []byte(subPayload)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		var msg map[string]interface{}
		if err := ws.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline failed: %v", err)
		}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if msg["event"] != "pusher:error" {
			t.Errorf("Expected pusher:error, got: %v", msg)
		}

		data, _ := msg["data"].(map[string]interface{})
		if code, ok := data["code"].(float64); !ok || int(code) != 4009 {
			t.Errorf("Expected error code 4009, got %v", data["code"])
		}
	})

	t.Run("Robustness_InvalidJSON", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()
		expectHandshake(t, ws)

		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{INVALID_JSON`)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		var msg map[string]interface{}
		if err := ws.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline failed waiting for error: %v", err)
		}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Failed to read error response: %v", err)
		}
		if msg["event"] != "pusher:error" {
			t.Errorf("Expected pusher:error response to garbage, got: %v", msg)
		}

		if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"event":"pusher:ping"}`)); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		if err := ws.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline failed waiting for pong: %v", err)
		}
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("Server died or didn't respond to ping after garbage: %v", err)
		}
		if msg["event"] != "pusher:pong" {
			t.Errorf("Expected pong, got %v", msg)
		}
	})
}

// Helper functions (duplicated for completeness in file update)
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

func waitForServer(url string) bool {
	for i := 0; i < 30; i++ {
		conn, err := http.Get(url)
		if err == nil {
			_ = conn.Body.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
