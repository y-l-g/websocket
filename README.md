# Pogo WebSocket

## Warning

This project is highly experimental, use with caution.

**The Native, High-Performance Real-Time Solution for PHP.**

* A Caddy module that embeds a scalable, Pusher-compatible WebSocket server directly into the FrankenPHP binary
* CGO-exported functions `pogo_websocket_publish` and `pogo_websocket_broadcast_multi` allow PHP to broadcast messages instantly via shared memory.
* The Caddy module uses FrankenPHP's `ExtensionWorker` API to invoke a dedicated pool of PHP threads for authentication, avoiding network overhead.

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

Install a ZTS version of libphp and xcaddy. Then, use xcaddy to build FrankenPHP with the frankenphp-pogo module:

```bash
CGO_CFLAGS="-D_GNU_SOURCE $(php-config --includes)" \
CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" \
CGO_ENABLED=1 \
xcaddy build \
    --output frankenphp \
    --with github.com/y-l-g/websocket=./mod \
    --with github.com/dunglas/frankenphp/caddy \
    --with github.com/dunglas/caddy-cbrotli
```

You can also use the frankenphp binary or the docker image provided in this repository (see packages and release)

### Step 2: Install the Laravel Broadcast Driver

```bash
composer require pogo/websocket
php artisan pogo:ws-install
```

## ⚙️ Configuration

Configure the module within your `Caddyfile` at the root of your laravel project (this exemple is an adapted copy of the octane Caddyfile, it will work with `php artisan octane:frankenphp --caddyfile=Caddyfile`).

```caddy
{
    {$CADDY_GLOBAL_OPTIONS}

    admin {$CADDY_SERVER_ADMIN_HOST}:{$CADDY_SERVER_ADMIN_PORT}

    frankenphp {
        worker {
            file "{$APP_PUBLIC_PATH}/frankenphp-worker.php"
            {$CADDY_SERVER_WORKER_DIRECTIVE}
            {$CADDY_SERVER_WATCH_DIRECTIVES}
        }
    }
    order pogo_websocket before php_server
}

{$CADDY_EXTRA_CONFIG}

{$CADDY_SERVER_SERVER_NAME} {
    log {
        level {$CADDY_SERVER_LOG_LEVEL}

    # Redact the authorization query parameter that can be set by Mercure...
        format filter {
            wrap {$CADDY_SERVER_LOGGER}
            fields {
                uri query {
                    replace authorization REDACTED
                }
            }
        }
    }

    route /app/* {
        pogo_websocket {
            app_id          pogo-app
            auth_path       /pogo/auth
            auth_script     {$APP_PUBLIC_PATH}/websocket-worker.php
            webhook_secret  super-secret-key
            
            handshake_rate  100         # New connection attempts per second (Default: 100)
            handshake_burst 50          # Burst allowance (Default: 50)
            max_connections 10000       # Max concurrent clients
            max_auth_body   16384       # Max PHP Auth response size (bytes)
            max_concurrent_auth 100     # Max concurrent PHP Auth requests (DoS Protection)
            
            num_workers     2           # Number of PHP workers dedicated to Auth
            num_shards      8           # Internal sharding (Default: 2 * CPU Cores)
            
            ping_period     54s         # Server Ping interval
            pong_wait       60s         # Client Pong timeout
            write_wait      10s         # Socket write timeout
            
            # redis_host      localhost:6379
        }
    }

    route {
        root * "{$APP_PUBLIC_PATH}"
        encode zstd br gzip

        # Mercure configuration is injected here...
        {$CADDY_SERVER_EXTRA_DIRECTIVES}

        php_server {
            index frankenphp-worker.php
            try_files {path} frankenphp-worker.php
            # Required for the public/storage/ directory...
            resolve_root_symlink
        }
    }
}
```

Fill your .env

```ini
BROADCAST_CONNECTION=pogo
WS_APP_ID=pogo-app
WS_APP_SECRET=super-secret-key #needed for pusher client but not really sensitive i guess

VITE_POGO_HOST=localhost #your site adress
VITE_POGO_PORT=80 #your site port
VITE_POGO_WSS_PORT=443 #your site port
```

Start octane (`frankenphp` must be compiled with `pogo`, use the `dockerfile` or the binary provided in this repo and put it in you bin folder).

```bash
php artisan octane:start --caddyfile=Caddyfile
```

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
| `pogo_websocket_subscriptions_total` | Counter | Total active subscriptions. |
| `pogo_websocket_auth_duration_seconds` | Histogram | Latency of the PHP Auth Worker. |
| `pogo_websocket_messages_dropped_total` | Counter | Messages dropped due to full client buffer. |

---

## 🛠 Troubleshooting

* **4100 Over Capacity:** Increase `max_connections` in Caddyfile.
* **4009 Connection Unauthorized:** Check `webhook_secret` matches `WS_APP_SECRET`.
* **Too Many Requests:** Tune `handshake_rate` if legitimate traffic is being blocked.
