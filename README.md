# Pogo WebSocket Engine

**The Native, High-Performance Real-Time Solution for PHP.**

The **Pogo WebSocket Engine** is a Caddy module written in Go that embeds a scalable, Pusher-compatible WebSocket server directly into the FrankenPHP binary. It eliminates the need for external Node.js sidecars.

---

## 🚀 Features

* **Pusher Protocol v7 Compliant:** Supports Private & Presence channels, and User Authentication.
* **High Performance:** Benchmarked at **550+ messages/sec** with sub-10ms latency on minimal hardware.
* **Zero-Copy Broadcasts:** Optimizes CPU usage by encoding messages once for thousands of clients.
* **DoS Protection:** Built-in Token Bucket Rate Limiting, Handshake Throttling, and Circuit Breakers for PHP Auth.
* **Horizontal Scaling:** Redis Pub/Sub support for multi-node clusters.

---

## 📦 Installation

### Step 1: Build the Binary

```bash
CGO_CFLAGS="-D_GNU_SOURCE $(php-config --includes)" \
CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" \
CGO_ENABLED=1 \
xcaddy build \
    --output frankenphp \
    --with github.com/y-l-g/websocket=. \
    --with github.com/dunglas/frankenphp/caddy \
    --with github.com/dunglas/caddy-cbrotli
```

### Step 2: Install the PHP Driver

```bash
composer require pogo/websocket
php artisan pogo:ws-install
```

---

## ⚙️ Configuration

### Caddyfile

Configure the module within your `Caddyfile`.

```caddy
{
    frankenphp
    order pogo_websocket before php_server
}

:80 {
    # Match the Pusher Protocol path
    route /app/* {
        pogo_websocket {
            # --- Identity ---
            app_id          pogo-app
            
            # --- Authentication ---
            auth_path       /pogo/auth
            auth_script     public/frankenphp-worker.php
            webhook_secret  super-secret-key  # REQUIRED for User Auth (pusher:signin)
            
            # --- Security & Limits ---
            handshake_rate  100         # New connection attempts per second (Default: 100)
            handshake_burst 50          # Burst allowance (Default: 50)
            max_connections 10000       # Max concurrent clients
            max_auth_body   16384       # Max PHP Auth response size (bytes)
            max_concurrent_auth 100     # Max concurrent PHP Auth requests (DoS Protection)
            
            # --- Tuning ---
            num_workers     2           # Number of PHP workers dedicated to Auth
            num_shards      8           # Internal sharding (Default: 2 * CPU Cores)
            
            # --- Timeouts ---
            ping_period     54s         # Server Ping interval
            pong_wait       60s         # Client Pong timeout
            write_wait      10s         # Socket write timeout
            
            # --- Clustering (Optional) ---
            # redis_host      localhost:6379
        }
    }
    
    # PHP Application
    php_server
}
```

### Laravel Configuration (`.env`)

```ini
BROADCAST_CONNECTION=pogo
WS_APP_ID=pogo-app
WS_APP_SECRET=super-secret-key

VITE_POGO_HOST=localhost
VITE_POGO_PORT=80
VITE_POGO_WSS_PORT=443
```

---

## 🔐 User Authentication (Sign-In)

Pogo supports the Pusher User Authentication flow (formerly "System Events"), allowing you to terminate specific user connections via the API.

---

## 📊 Metrics

Prometheus metrics are available at `http://localhost:2019/metrics` (Caddy Admin):

| Metric | Type | Description |
| :--- | :--- | :--- |
| `pogo_websocket_connections_active` | Gauge | Active TCP connections. |
| `pogo_websocket_messages_total` | Counter | Total messages broadcasted. |
| `pogo_websocket_auth_failures_total` | Counter | Failed auths (labels: `concurrency_limit`, `worker_error`). |
| `pogo_websocket_circuit_breaker_open_total` | Counter | Requests rejected by Circuit Breaker. |
| `pogo_websocket_broker_dropped_total` | Counter | Messages dropped due to internal backpressure. |

---

## 🛠 Troubleshooting

* **4100 Over Capacity:** Increase `max_connections` in Caddyfile.
* **4009 Connection Unauthorized:** Check `webhook_secret` matches `WS_APP_SECRET`.
* **Too Many Requests:** Tune `handshake_rate` if legitimate traffic is being blocked.
