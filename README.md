# Pogo WebSocket Engine

**The Native, High-Performance Real-Time Solution for PHP.**

The **Pogo WebSocket Engine** is a Caddy module written in Go that embeds a scalable, Pusher-compatible WebSocket server directly into the FrankenPHP binary.

It eliminates the need for external Node.js sidecars or separate PHP daemons. By leveraging Go’s concurrency for connection management and FrankenPHP’s Worker API for authentication, it delivers near-zero latency while keeping your business logic in PHP.

---

## 🏗 Technical Architecture

### Tech Stack
*   **Core Engine:** Go
*   **Protocol:** Pusher Protocol v7
*   **Scaling Layer:** Redis Pub/Sub
*   **Observability:** Prometheus

### 1. Hexagonal Architecture & Isolation
The Go `Hub` manages the WebSocket lifecycle (TCP, Pings, Fan-out). It knows nothing about your application domain. It communicates with PHP only via strictly defined ports:
*   **Inbound (PHP -> Go):** A CGO-exported function `pogo_websocket_publish` allows PHP to broadcast messages instantly via shared memory.
*   **Outbound (Go -> PHP):** The `WorkerAuthProvider` uses FrankenPHP's `SendRequest` API to invoke a dedicated pool of PHP threads for authentication, avoiding network overhead.

### 2. Sharding & Concurrency
To prevent lock contention on the central Hub during high loads, the system utilizes a **32-Shard Architecture**:
*   **Parallel Handshakes:** Client registration is dispatched immediately to shards based on `fnv32(client_id) % 32`.
*   **Lock-Free Broadcasting:** Messages are routed to shards based on channel names, allowing independent broadcasting routines to run in parallel.
*   **Graceful Shutdown:** The engine utilizes `sync.WaitGroup` barriers. On SIGTERM, it stops accepting new connections, sends a `1001 Going Away` frame to all clients, and waits for sockets to drain before exiting.

### 3. Resilience & Security
*   **Circuit Breaker:** Authentication requests are protected. If the PHP backend stalls, the Go layer "fails fast" to prevent a thundering herd of goroutines from exhausting system resources.
*   **Memory Pooling:** To offset the cost of real-time auth, the `WorkerAuthProvider` utilizes `sync.Pool` for HTTP Recorders, drastically reducing Garbage Collection pressure during connection storms.

### 4. Distributed Scaling
*   **Memory Broker (Default):** For single-server setups, messages flow through Go channels.
*   **Redis Broker:** For Cluster setups, the engine switches to a Redis Pub/Sub adapter. It features an **Exponential Backoff Reconnection Loop**, ensuring the process survives Redis outages without crashing.

---

## 📦 Installation

### Step 1: Build the Binary
You must compile FrankenPHP with this custom module included.

```bash
CGO_CFLAGS="-D_GNU_SOURCE $(php-config --includes)" \
CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" 
CGO_ENABLED=1 
xcaddy build \
    --output frankenphp \
    --with github.com/y-l-g/websocket=. \
    --with github.com/dunglas/frankenphp/caddy \
    --with github.com/dunglas/caddy-cbrotli
```

### Step 2: Install the PHP Driver
In your Laravel application:

```bash
composer require pogo/websocket
```

Run the automated installer. This command will:
1.  Install frontend dependencies (`laravel-echo`, `pusher-js`).
2.  Configure `config/broadcasting.php`.
3.  Update your `.env`.

```bash
php artisan pogo:ws-install
```

---

## ⚙️ Configuration

### Caddyfile Configuration
The module is configured via the `pogo_websocket` directive within your `Caddyfile`. Use a dedicated route to intercept WebSocket traffic.

```caddy
{
    frankenphp
    order pogo_websocket before pogo
}

localhost {
    # Match the Pusher Protocol path
    route /app/* {
        pogo_websocket {
            # REQUIRED: Unique ID for this app
            app_id          pogo-app
            
            # REQUIRED: Internal route for authentication (Go -> PHP)
            auth_path       /pogo/auth
            
            # REQUIRED: Path to the worker script 
            auth_script     public/frankenphp-worker.php
            
            # Tuning
            num_workers     2
            
            # Clustering (Optional)
            # redis_host      localhost:6379
            
            # Webhooks (Optional)
            # webhook_url     http://localhost/webhook
            # webhook_secret  my-secret-key
        }
    }
    
    php_server
}
```

### Laravel Configuration (`.env`)

The installer sets these automatically, but ensure they match your `Caddyfile`.

```ini
BROADCAST_CONNECTION=pogo
WS_APP_ID=pogo-app

# Frontend Configuration
VITE_POGO_HOST=localhost
VITE_POGO_PORT=8000
VITE_POGO_WSS_PORT=443
```

---

## 💻 Usage Guide

### Starting the Server
Run the compiled binary. It will start Caddy, the PHP Application, and the WebSocket Engine simultaneously.

```bash
./frankenphp run --config Caddyfile
```

### Client-Side (JavaScript)
The server is fully compatible with `pusher-js` and `laravel-echo`. The installer provides a configured `echo.js`.

```javascript
import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

window.Pusher = Pusher;

window.Echo = new Echo({
    broadcaster: 'pusher',
    // Dummy values required by pusher-js (ignored by server)
    key: 'key',
    cluster: 'cluster',
    
    // Dynamic Configuration via Vite
    wsHost: import.meta.env.VITE_POGO_HOST || window.location.hostname,
    wsPort: import.meta.env.VITE_POGO_PORT || 80,
    wssPort: import.meta.env.VITE_POGO_WSS_PORT || 443,
    
    forceTLS: false,
    disableStats: true,
    enabledTransports: ['ws', 'wss'],
});
```

---

## 📡 API & Core Functionality

### 1. Publishing Events (PHP)
Use the standard Laravel Event system. The driver automatically routes this to the native CGO function.

```php
// app/Events/MessageSent.php
class MessageSent implements ShouldBroadcast {
    public function broadcastOn() {
        return new PrivateChannel('room.1');
    }
}

// Trigger
event(new MessageSent($message));
```

### 2. Presence Channels
The engine handles complex Presence logic (member tracking) entirely in Go.

**PHP (`routes/channels.php`):**
```php
Broadcast::channel('chat.{id}', function ($user, $id) {
    // Return array containing user info
    return ['id' => $user->id, 'name' => $user->name];
});
```

**WebSocket Events (Pusher Protocol):**
*   `pusher_internal:subscription_succeeded`: Sent to the user connecting. Contains the full list of current members.
*   `pusher_internal:member_added`: Broadcast to others when a *new* user joins.
*   `pusher_internal:member_removed`: Broadcast when the *last* connection for a user closes.

### 3. Client Events ("Whispers")
Clients can send messages directly to other clients without hitting the PHP backend.
*   **Requirement:** Channel must be `private-*` or `presence-*`.
*   **Event Name:** Must start with `client-`.

```javascript
// JS Client
channel.whisper('typing', {
    user: 'John'
});
```

---

## 📊 Observability (Metrics)

Metrics are injected directly into Caddy's registry. They are available at the standard Caddy admin endpoint: `http://localhost:2019/metrics`.

| Metric Name | Type | Description |
| :--- | :--- | :--- |
| `pogo_websocket_connections_active` | Gauge | Current active TCP connections. |
| `pogo_websocket_messages_total` | Counter | Total messages broadcasted. |
| `pogo_websocket_subscriptions_total` | Counter | Total active subscriptions. |
| `pogo_websocket_auth_duration_seconds` | Histogram | Latency of the PHP Auth Worker. |
| `pogo_websocket_circuit_breaker_open_total` | Counter | Number of requests rejected due to PHP/DB failure. |
| `pogo_websocket_auth_failures_total` | Counter | 500/Timeouts from PHP. |

---

## 🛠 Maintenance & Contribution

### Automation (Makefile)
We use a `Makefile` to enforce the strict compile/test loop.

*   `make build`: Compiles the binary.
*   `make test`: Runs the Go unit test suite (including Sharding and Circuit Breaker verification).
*   `make demo`: Runs the example app.

---