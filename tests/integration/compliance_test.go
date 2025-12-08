package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestCompliance(t *testing.T) {
	rootDir, _ := filepath.Abs("../../")
	binPath := os.Getenv("FRANKENPHP_BINARY")
	if binPath == "" {
		binPath = filepath.Join(rootDir, "frankenphp")
	}

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skipf("Skipping Compliance test: binary not found at %s", binPath)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	caddyfileContent, err := os.ReadFile("fixtures/Caddyfile")
	if err != nil {
		t.Fatalf("Failed to read fixture: %v", err)
	}

	configStr := string(caddyfileContent)
	configStr = strings.ReplaceAll(configStr, ":9090", fmt.Sprintf(":%d", port))
	configStr = strings.ReplaceAll(configStr, "app_id          test-app", "app_id          test-app\n            webhook_secret  super-secret-key")

	tmpCaddyfile, err := os.CreateTemp("", "Caddyfile.*")
	if err != nil {
		t.Fatalf("Failed to create temp Caddyfile: %v", err)
	}
	defer func() { _ = os.Remove(tmpCaddyfile.Name()) }()

	if _, err := tmpCaddyfile.WriteString(configStr); err != nil {
		t.Fatalf("Failed to write temp Caddyfile: %v", err)
	}
	_ = tmpCaddyfile.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "run", "--config", tmpCaddyfile.Name())
	cmd.Dir = rootDir
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
		t.Fatalf("Server failed to start")
	}

	t.Run("Protocol_Version_Check", func(t *testing.T) {
		oldProtoURL := wsURL + "?protocol=4"
		_, resp, err := websocket.DefaultDialer.Dial(oldProtoURL, nil)

		if err == nil {
			t.Error("Expected error connecting with protocol=4, got success")
		} else if resp != nil && resp.StatusCode != 400 {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Binary_Frame_Rejection", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }()

		expectHandshake(t, ws)

		if err := ws.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02}); err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		_, _, err = ws.ReadMessage()
		if err == nil {
			t.Error("Expected read error (close), got nil")
		}

		closeErr, ok := err.(*websocket.CloseError)
		if !ok {
			t.Errorf("Expected CloseError, got %T: %v", err, err)
		} else if closeErr.Code != 4003 {
			t.Errorf("Expected close code 4003, got %d", closeErr.Code)
		}
	})

	t.Run("Pusher_Signin", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Dial failed: %v", err)
		}
		defer func() { _ = ws.Close() }() // Fixed

		var connectMsg map[string]interface{}
		if err := ws.ReadJSON(&connectMsg); err != nil {
			t.Fatalf("Failed to read handshake: %v", err)
		}

		dataStr, _ := connectMsg["data"].(string)
		var innerData map[string]interface{}
		_ = json.Unmarshal([]byte(dataStr), &innerData)
		socketID, _ := innerData["socket_id"].(string)

		if socketID == "" {
			t.Fatal("Could not extract socket_id")
		}

		secret := "super-secret-key"
		userData := `{"id":"123","name":"Test User"}`
		toSign := fmt.Sprintf("%s::user::%s", socketID, userData)

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(toSign))
		signature := hex.EncodeToString(mac.Sum(nil))
		authSig := fmt.Sprintf("test-app:%s", signature)

		signinMsg := map[string]interface{}{
			"event": "pusher:signin",
			"data": map[string]string{
				"auth":      authSig,
				"user_data": userData,
			},
		}
		if err := ws.WriteJSON(signinMsg); err != nil {
			t.Fatalf("Write signin failed: %v", err)
		}

		var resp map[string]interface{}
		if err := ws.ReadJSON(&resp); err != nil {
			t.Fatalf("Read response failed: %v", err)
		}

		if resp["event"] != "pusher:signin_success" {
			t.Errorf("Expected signin_success, got %v", resp)
		}

		data, _ := resp["data"].(map[string]interface{})
		respUserData, _ := data["user_data"].(string)
		if respUserData != userData {
			t.Errorf("User data mismatch. Got %s", respUserData)
		}
	})
}
