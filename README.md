# FrankenPHP WebSocket Engine

**The Native, High-Performance Real-Time Solution for PHP.**

The **FrankenPHP WebSocket Engine** is a Caddy module written in Go that embeds a scalable, Pusher-compatible WebSocket server directly into the FrankenPHP binary.

It eliminates the need for external Node.js sidecars (Laravel Echo Server, Soketi) or separate PHP daemons (Laravel Reverb, Swoole). By leveraging Go’s concurrency for connection management and FrankenPHP’s Worker API for authentication, it delivers near-zero latency and massive scalability while keeping your business logic in PHP.

---

## Technical Architecture

### Tech Stack
*   **Core Engine:** Go
*   **Web Server:** Caddy v2 (via FrankenPHP)
*   **Language Bridge:** CGO (Shared Memory)
*   **Protocol:** Pusher Protocol v7 (WebSocket)
*   **Scaling Layer:** Redis Pub/Sub (Optional, for clustering)
*   **Observability:** Prometheus

1.  **Hexagonal Architecture:** The Go `Hub` manages the WebSocket lifecycle (TCP, Pings, Fan-out). It knows nothing about your application domain. It communicates with PHP only via strictly defined ports:
    *   **Inbound:** A CGO-exported function `frankenphp_websocket_publish` allows PHP to broadcast messages instantly.
    *   **Outbound:** The `WorkerAuthProvider` uses FrankenPHP's `SendRequest` API to invoke a dedicated pool of PHP threads for authentication, avoiding network overhead.

2.  **Sharding & Concurrency:**
    *   To prevent lock contention on the central Hub during high loads, the system creates **32 Hub Shards**.
    *   Incoming connections and messages are routed to shards based on `fnv32(channel_name) % 32`.
    *   This allows the system to handle tens of thousands of concurrent connections on a single node without blocking.

3.  **Resilience (Circuit Breaker):**
    *   Authentication requests are protected by a **Circuit Breaker**. If the PHP backend (e.g., MySQL) stalls, the Go layer "fails fast" to prevent a thundering herd of goroutines from exhausting system resources.
    *   **Auth Caching:** Successful authentications are cached in memory (TTL 30s) to absorb reconnection storms.

4.  **Distributed Scaling:**
    *   **Memory Broker (Default):** For single-server setups, messages flow through Go channels.
    *   **Redis Broker:** For Kubernetes/Cluster setups, the engine switches to a Redis Pub/Sub adapter, allowing users on Node A to communicate with users on Node B transparently.


## Installation


### Step 1: Build the Binary
You must compile FrankenPHP with this custom module included.

```bash
CGO_CFLAGS="-D_GNU_SOURCE $(php-config --includes)" \
CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" 
CGO_ENABLED=1 
xcaddy build     
    --output frankenphp     
    --with github.com/pogo/websocket=.
    --with github.com/dunglas/frankenphp/caddy     
    --with github.com/dunglas/caddy-cbrotli
```

### Step 2: Install the PHP Driver
In your Laravel application:

```bash
composer require pogo/websocket
```

Run the install command to publish the worker script:

```bash
php artisan pogo:ws-install
```
*This creates `websocket-worker.php` in your project root.*

---

## 5. Configuration

### Caddyfile Configuration
The module is configured via the `frankenphp_websocket` directive within your `Caddyfile`.

```caddy
{
    # Enable Debug logs to see Auth Cache hits
    log {
        level DEBUG
    }
    
    frankenphp
    order frankenphp_websocket before frankenphp
}

:8000 {
    route /ws {
        frankenphp_websocket {
            auth_path       /frankenphp/auth
            auth_script     frankenphp-worker.php
            num_workers     2
            webhook_url     http://localhost:8000/api/webhook
            webhook_secret  my-secret-key
            redis_host      localhost:6379
        }
    }
    
    php_server
}
```

| Directive | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `auth_path` | String | `/broadcasting/auth` | The internal route used to trigger the Laravel Auth logic. |
| `auth_script` | Path | `worker.php` | Path to the PHP script that handles auth requests. |
| `num_workers` | Int | `2` | Number of dedicated PHP threads for authentication. |
| `redis_host` | String | `""` | Redis address (e.g., `localhost:6379`). If empty, uses Memory. |
| `webhook_url` | URL | `""` | URL to receive `channel_occupied` / `vacated` events. |
| `webhook_secret` | String | `""` | Secret key for HMAC-SHA256 signature on webhooks. |

### Laravel Configuration (`.env`)

```ini
BROADCAST_CONNECTION=frankenphp
```

Ensure `config/broadcasting.php` includes:

```php
'connections' => [
    'frankenphp' => [
        'driver' => 'frankenphp',
    ],
],
```

---

## 6. Usage Guide (Development)

### Starting the Server
Run the compiled binary. It will start Caddy, the PHP Application, and the WebSocket Engine simultaneously.

```bash
./frankenphp run --config Caddyfile
```

### Client-Side (JavaScript)
The server is fully compatible with `pusher-js` and `laravel-echo`.

```javascript
import Echo from 'laravel-echo';
import Pusher from 'pusher-js';

window.Pusher = Pusher;

window.Echo = new Echo({
    broadcaster: 'pusher',
    key: 'any-key', // Key is ignored by the engine but required by the client
    wsHost: window.location.hostname,
    wsPort: 8000,
    wssPort: 443,
    forceTLS: false,
    disableStats: true,
    enabledTransports: ['ws', 'wss'],
    // Ensure cookies are sent for Auth
    authEndpoint: '/frankenphp/auth', 
});

// Subscribe
window.Echo.private(`room.${roomId}`)
    .listen('NewMessage', (e) => {
        console.log(e.message);
    });
```

---

## 7. API Documentation / Core Functionality

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
The engine handles complex Presence logic (member tracking).

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

### 4. Metrics (Prometheus)
Metrics are exposed at `http://localhost:2019/metrics` (Caddy Admin Interface).

| Metric Name | Type | Description |
| :--- | :--- | :--- |
| `frankenphp_ws_connections_active` | Gauge | Active TCP connections. |
| `frankenphp_ws_messages_total` | Counter | Total messages broadcasted. |
| `frankenphp_ws_auth_seconds` | Histogram | Latency of the PHP Auth Worker. |
| `frankenphp_ws_circuit_breaker_open_total` | Counter | Number of requests rejected due to backend failure. |

---

## 8. Deployment

### Docker (Multi-Stage Build)

```dockerfile
# Stage 1: Builder
FROM dunglas/frankenphp:latest-builder AS builder

# Build with extension
COPY src/go /go/src/app/
RUN xcaddy build \
    --output /usr/local/bin/frankenphp \
    --with github.com/dunglas/frankenphp \
    --with github.com/your-org/frankenphp-websocket=/go/src/app

# Stage 2: Runner
FROM dunglas/frankenphp:latest-php8.3

# Copy Binary
COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp

# Copy App & Worker
COPY . /app
COPY frankenphp-worker.php /app/frankenphp-worker.php

CMD ["frankenphp", "run", "--config", "/app/Caddyfile"]
```

### Kubernetes
For HA (High Availability), deploy multiple replicas of the Docker container and configure `redis_host` in the Caddyfile to point to a shared Redis instance. The engine will automatically sync broadcasts across pods.

---

## 9. Maintenance & Contribution

### Coding Standards
*   **Go:** Follow `gofmt` and standard Go idioms. Run `go mod tidy` before committing.
*   **PHP:** Follow PSR-12 coding standards.

### Safety Guidelines
1.  **CGO:** Never pass Go pointers to C/PHP. Always copy data strings immediately (`frankenphp.GoString`).
2.  **Locks:** Do not use the `GlobalHub` lock for long operations. Use the sharded locks in `SubscriptionManager`.
3.  **Metrics:** Always initialize new metrics to 0 in `RegisterMetrics`.

### Testing
*   **Unit Tests:** `go test ./...`
*   **Integration:** Use the `demo/` application. Open `public/presence_test.html` in multiple browser tabs to verify concurrency and presence logic.