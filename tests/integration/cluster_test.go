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
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
)

func TestCluster(t *testing.T) {
	rootDir, _ := filepath.Abs("../../")
	binPath := os.Getenv("FRANKENPHP_BINARY")
	if binPath == "" {
		binPath = filepath.Join(rootDir, "frankenphp")
	}

	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Skipf("Skipping Cluster test: binary not found at %s", binPath)
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()
	t.Logf("Shared Redis started at %s", mr.Addr())

	node1 := startNode(t, binPath, rootDir, mr.Addr(), "node1")
	defer node1.Stop()

	node2 := startNode(t, binPath, rootDir, mr.Addr(), "node2")
	defer node2.Stop()

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/app/test-app", node2.Port)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial Node 2 failed: %v", err)
	}
	defer func() { _ = ws.Close() }()

	var connectMsg map[string]interface{}
	_ = ws.ReadJSON(&connectMsg)

	subPayload := `{"event":"pusher:subscribe","data":{"channel":"cluster-test"}}`
	if err := ws.WriteMessage(websocket.TextMessage, []byte(subPayload)); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	var subMsg map[string]interface{}
	_ = ws.ReadJSON(&subMsg)

	params := url.Values{}
	params.Add("app_id", "test-app")
	params.Add("channel", "cluster-test")
	params.Add("event", "cross-node-event")
	params.Add("data", `{"from":"node1"}`)

	pubUrl := fmt.Sprintf("http://127.0.0.1:%d/publish/publish.php?%s", node1.Port, params.Encode())
	resp, err := http.Get(pubUrl)
	if err != nil {
		t.Fatalf("Publish to Node 1 failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Errorf("Node 1 Publish returned %d", resp.StatusCode)
	}

	if err := ws.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline failed: %v", err)
	}

	var msg map[string]interface{}
	if err := ws.ReadJSON(&msg); err != nil {
		t.Fatalf("Cluster broadcast failed: %v", err)
	}

	if msg["event"] != "cross-node-event" {
		t.Errorf("Expected 'cross-node-event', got %v", msg["event"])
	}
}

type TestNode struct {
	Port   int
	Cmd    *exec.Cmd
	Cancel context.CancelFunc
}

func (n *TestNode) Stop() {
	n.Cancel()
	_ = n.Cmd.Wait()
}

func startNode(t *testing.T, binPath, rootDir, redisAddr, name string) *TestNode {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("[%s] Failed to get port: %v", name, err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	config := fmt.Sprintf(`
{
    frankenphp
    order pogo_websocket before php_server
    admin off
}

:%d {
    route /app/* {
        pogo_websocket {
            app_id          test-app
            auth_path       /auth
            auth_script     tests/integration/fixtures/worker.php
            num_workers     1
            redis_host      %s
        }
    }

    route /publish/* {
        root * tests/integration/fixtures
        php_server
    }
    route /auth {
        root * tests/integration/fixtures
        php_server
    }
}
`, port, redisAddr)

	tmpCaddyfile, err := os.CreateTemp("", "Caddyfile."+name+".*")
	if err != nil {
		t.Fatalf("[%s] Failed to create Caddyfile: %v", name, err)
	}

	if _, err := tmpCaddyfile.WriteString(config); err != nil {
		t.Fatalf("[%s] Write config failed: %v", name, err)
	}
	_ = tmpCaddyfile.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath, "run", "--config", tmpCaddyfile.Name())
	cmd.Dir = rootDir

	if err := cmd.Start(); err != nil {
		t.Fatalf("[%s] Start failed: %v", name, err)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/publish/publish.php", port)
	ready := false
	for i := 0; i < 20; i++ {
		if resp, err := http.Get(url); err == nil {
			// Fixed: Ignore close error
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !ready {
		cancel()
		t.Fatalf("[%s] Node failed to start on port %d", name, port)
	}

	return &TestNode{
		Port:   port,
		Cmd:    cmd,
		Cancel: cancel,
	}
}
