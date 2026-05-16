# Pogo WebSocket

## Warning

This project is highly experimental, use with caution.

**The Native, High-Performance Real-Time Solution for PHP.**

- A Caddy module that embeds a scalable, Pusher-compatible WebSocket server directly into the FrankenPHP binary
- CGO-exported functions `pogo_websocket_publish` and `pogo_websocket_broadcast_multi` allow PHP to broadcast messages instantly and return native status codes for precise failures.
- The Caddy module uses FrankenPHP's `ExtensionWorker` API to invoke a dedicated pool of PHP threads for authentication, avoiding network overhead.

---

## Repository Layout

This repository is intentionally limited to the websocket extension and its package-level validation:

- `module/`: the Go/Caddy/FrankenPHP websocket module.
- `lib/`: the Laravel broadcasting driver.
- `module/tests/`: module unit, integration, and low-level performance tests.

Full application showcases belong in `pogoShowcase`. Keep this repository focused on code that ships with, tests, or measures the websocket package.

---

## Features

- **Pusher Protocol v7 Compliant:** Supports Private & Presence channels, and User Authentication.
- **High Performance:** Benchmarked at **550+ messages/sec** with sub-10ms latency on minimal hardware. See [`benchmarks/`](benchmarks/) for the reproducible harness.
- **Zero-Copy Broadcasts:** Optimizes CPU usage by encoding messages once for thousands of clients.
- **DoS Protection:** Built-in Token Bucket Rate Limiting, Handshake Throttling, and Circuit Breakers for PHP Auth.
- **Horizontal Scaling:** Redis Pub/Sub support for multi-node clusters.

---

## Installation

### Step 1: Docker

Build a FrankenPHP binary that includes Pogo WebSocket with `xcaddy`. See the official
[FrankenPHP Docker documentation](https://frankenphp.dev/docs/docker/) for the
base image details.

Example Dockerfile from this repository root:

```dockerfile
FROM dunglas/frankenphp:builder AS builder

COPY --from=caddy:builder /usr/bin/xcaddy /usr/bin/xcaddy
COPY . /src/websocket

RUN CGO_ENABLED=1 \
    XCADDY_SETCAP=1 \
    XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx" \
    CGO_CFLAGS="$(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
        --output /usr/local/bin/frankenphp \
        --with github.com/dunglas/frankenphp=./ \
        --with github.com/dunglas/frankenphp/caddy=./caddy  \
        --with github.com/dunglas/caddy-cbrotli \
        --with github.com/y-l-g/websocket/module=./src/websocket/module

FROM dunglas/frankenphp AS runner

COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp
```

Then copy your app and `Caddyfile` into the runner image as usual.

### Step 2: Install the Laravel Broadcast Driver

```bash
composer require pogo/websocket
php artisan pogo:ws-install
```

## Configuration

Configure the module within your `Caddyfile` at the root of your laravel project (this exemple is an adapted copy of the octane Caddyfile, it will work with `php artisan octane:frankenphp --caddyfile=Caddyfile`).

```caddy
{
    frankenphp {
        worker {
            file "public/frankenphp-worker.php"
        }
    }
    order pogo_websocket before php_server
}

:8080 {
    log {
        level info
    }

    format filter {
        wrap json
        fields {
            uri query {
                replace authorization REDACTED
            }
        }
    }

    route /app/* {
        pogo_websocket {
            app_id          pogo-app
            app_secret      {$WS_APP_SECRET}
            auth_path       /pogo/auth
            auth_script     public/websocket-worker.php
            webhook_secret  {$POGO_WEBHOOK_SECRET}
            allowed_origins https://app.example.com https://admin.example.com

            handshake_rate  100         # New connection attempts per second (Default: 100)
            handshake_burst 50          # Burst allowance (Default: 50)
            max_connections 10000       # Max concurrent clients
            max_auth_body   16384       # Max PHP Auth response size (bytes)
            max_concurrent_auth 100     # Max concurrent PHP Auth requests (DoS Protection)
            broker_queue_size 1024      # Internal broker queue before publish fails fast
            shard_queue_size 1024       # Per-shard control/broadcast queue

            num_workers     2           # Number of PHP workers dedicated to Auth
            num_shards      8           # Internal sharding (Default: 2 * CPU Cores)

            ping_period     54s         # Server Ping interval
            pong_wait       60s         # Client Pong timeout
            write_wait      10s         # Socket write timeout

            # redis_host      localhost:6379
        }
    }

    route {
        root * public
        encode zstd br gzip

        php_server {
            index frankenphp-worker.php
            try_files {path} frankenphp-worker.php
            resolve_root_symlink
        }
    }
}
```

By default, WebSocket upgrades accept requests without an `Origin` header and browser
requests whose `Origin` host matches the request host. Configure `allowed_origins`
when your frontend connects from a different origin; entries must be exact
`http://` or `https://` origins, including the port when one is used.

Native publish functions return `0` on success. Nonzero status codes indicate:
`1` hub missing, `2` channel too long, `3` event too long, `4` payload too large,
`5` invalid payload JSON, `6` broker publish failed, and `7` invalid multi-channel
JSON, `8` broker queue full, and `9` shard queue full. The Laravel broadcaster
turns these failures into `BroadcastException`.

Fill your .env

```ini
BROADCAST_CONNECTION=pogo
WS_APP_ID=pogo-app
WS_APP_SECRET=change-me-to-a-long-random-secret
POGO_WEBHOOK_SECRET=change-me-to-a-different-random-secret

VITE_POGO_APP_KEY="${WS_APP_ID}"
VITE_POGO_HOST=localhost #your site adress
VITE_POGO_PORT=80 #your site port
VITE_POGO_WSS_PORT=443 #your site port
```

Start octane (`frankenphp` must be compiled with `pogo_websocket`).

```bash
php artisan octane:start --caddyfile=Caddyfile
# or
frankenphp run --caddyfile=Caddyfile
# or
frankenphp run --config Caddyfile
```

## Metrics

Prometheus metrics are available at `http://localhost:2019/metrics` (Caddy Admin):

Set `POGO_WS_HOT_PATH_METRICS=true` to enable detailed per-message fanout, queue-depth, and write-duration histograms.

| Metric                                         | Type      | Description                                                 |
| :--------------------------------------------- | :-------- | :---------------------------------------------------------- |
| `pogo_websocket_connections_active`            | Gauge     | Active TCP connections.                                     |
| `pogo_websocket_messages_total`                | Counter   | Total messages broadcasted.                                 |
| `pogo_websocket_auth_failures_total`           | Counter   | Failed auths (labels: `concurrency_limit`, `worker_error`). |
| `pogo_websocket_circuit_breaker_open_total`    | Counter   | Requests rejected by Circuit Breaker.                       |
| `pogo_websocket_broker_dropped_messages_total` | Counter   | Messages dropped due to internal backpressure.              |
| `pogo_websocket_subscriptions_total`           | Counter   | Total active subscriptions.                                 |
| `pogo_websocket_auth_duration_seconds`         | Histogram | Latency of the PHP Auth Worker.                             |
| `pogo_websocket_client_dropped_messages_total` | Counter   | Messages dropped due to full client buffer.                 |
| `pogo_websocket_publish_failures_total`        | Counter   | Failed publish attempts by app and reason.                  |
| `pogo_websocket_webhook_dropped_total`         | Counter   | Webhook notifications dropped by reason.                    |

---

## Troubleshooting

- **4100 Over Capacity:** Increase `max_connections` in Caddyfile.
- **4009 Connection Unauthorized:** Check `app_secret` matches `WS_APP_SECRET`.
- **Too Many Requests:** Tune `handshake_rate` if legitimate traffic is being blocked.
