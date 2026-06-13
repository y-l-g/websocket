# Pogo WebSocket

**A native Reverb-compatible WebSocket runtime for FrankenPHP applications.**

- A Caddy module that embeds a scalable, Pusher-compatible WebSocket server directly into the FrankenPHP binary
- A Laravel `pogo` broadcaster that publishes through CGO-exported native functions instead of HTTP
- Optional FrankenPHP `ExtensionWorker` authentication fallback for clients that
  do not send standard Pusher channel signatures.

---

## Repository Layout

This repository is intentionally limited to the websocket extension and its package-level validation:

- `module/`: the Go/Caddy/FrankenPHP websocket module.
- `lib/`: the Laravel installer/package helpers and native broadcaster.
- `module/tests/`: module unit, integration, and low-level performance tests.

Full application showcases belong in `pogoShowcase`. Keep this repository focused on code that ships with, tests, or measures the websocket package.

---

## Features

- **Pusher-Compatible Protocol Subset:** Supports public, private, and presence channels, client events, and user authentication for Echo/Pusher-style clients.
- **Native Laravel Publishing:** The installed `pogo` broadcaster calls
  `pogo_websocket_broadcast_multi` / `pogo_websocket_publish` directly and turns
  native status codes into `BroadcastException`.
- **Reverb-compatible HTTP Publishing:** The runtime also accepts signed Pusher
  HTTP `POST /apps/{appId}/events` and `POST /apps/{appId}/batch_events`
  requests for compatibility with external publishers.
- **Reverb-compatible Management API:** Supports signed channel, presence user,
  connection count, and user termination endpoints for local-process state.
- **Benchmark Harness:** A reproducible benchmark setup is available in the
  `benchmarks/` workspace. Current results are experimental and
  topology-specific, so this README intentionally does not quote headline
  performance numbers.
- **Prepared Broadcast Fanout:** Optimizes CPU usage by encoding broadcast payloads once per channel fanout.
- **DoS Protection:** Built-in Token Bucket Rate Limiting, Handshake Throttling, and Circuit Breakers for PHP Auth.
- **Horizontal Scaling:** Redis Pub/Sub support for multi-node clusters with at-most-once delivery semantics.

## Production status

Pogo WebSocket targets Laravel applications running on FrankenPHP where the
primary publish path can run in the same native process as the WebSocket hub.
The default install uses Laravel's `pogo` broadcaster and the browser still uses
Echo's Reverb/Pusher-compatible protocol. Validate capacity, observability, and
failure modes for your topology before using it with production traffic.

Supported Pusher protocol behavior is intentionally scoped: connection
establishment, ping/pong, public/private/presence subscriptions, client events on
private and presence channels, `pusher:signin`, and signed HTTP event/batch
publishing. Reverb/Pusher management endpoints for channels, channel users,
connection counts, and user termination are implemented for the local process.
Features such as durable delivery, replay, encrypted channels, watchlists,
statistics APIs, and cluster-wide management aggregation are not implemented.

### Publishing paths

Pogo has one installed default path:

- `BROADCAST_CONNECTION=pogo` uses the native CGO functions. Use it for
  `ShouldBroadcastNow` or other broadcasts executed by the FrankenPHP/Pogo
  process that owns the active hub. There is no HTTP fallback: if the native
  extension or hub is missing, the broadcaster fails with a clear
  `BroadcastException`.

The runtime also exposes a compatibility path:

- `BROADCAST_CONNECTION=reverb` can be configured manually if broadcasts must be
  sent from a separate CLI queue worker, another container, or another service.
  That path uses Laravel's Reverb/Pusher HTTP publisher and requires the Pusher
  PHP SDK. It is useful, but it is not the native Pogo path.

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
    XCADDY_GO_BUILD_FLAGS="-ldflags='-w -s' -tags=nobadger,nomysql,nopgx,nowatcher" \
    CGO_CFLAGS="$(php-config --includes)" \
    CGO_LDFLAGS="$(php-config --ldflags) $(php-config --libs)" \
    xcaddy build \
        --output /usr/local/bin/frankenphp \
        --with github.com/dunglas/frankenphp@v1.12.4 \
        --with github.com/dunglas/frankenphp/caddy@v1.12.4 \
        --with github.com/dunglas/caddy-cbrotli@v1.0.1 \
        --with github.com/y-l-g/websocket/module=./src/websocket/module

FROM dunglas/frankenphp AS runner

COPY --from=builder /usr/local/bin/frankenphp /usr/local/bin/frankenphp
```

Then copy your app and `Caddyfile` into the runner image as usual.

### Step 2: Install the Laravel package

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

    @websocket path /app/* /apps/* /up /pogo/health
    route @websocket {
        pogo_websocket {
            app_id          {$REVERB_APP_ID}
            app_key         {$REVERB_APP_KEY}
            app_secret      {$REVERB_APP_SECRET}
            webhook_secret  {$POGO_WEBHOOK_SECRET}
            allowed_origins https://app.example.com https://admin.example.com

            handshake_rate  100         # New connection attempts per second (Default: 100)
            handshake_burst 50          # Burst allowance (Default: 50)
            max_connections 10000       # Max concurrent clients
            max_auth_body   16384       # Max PHP Auth response size (bytes)
            max_concurrent_auth 100     # Max concurrent PHP Auth requests (DoS Protection)
            broker_queue_size 1024      # Internal broker queue before publish fails fast
            shard_queue_size 1024       # Per-shard control/broadcast queue

            # auth_script     public/frankenphp-worker.php
            # auth_path       /broadcasting/auth
            # num_workers     2         # Optional PHP auth fallback workers
            num_shards      8           # Internal sharding (Default: 2 * CPU Cores)

            ping_period     54s         # Server Ping interval
            pong_wait       60s         # Client Pong timeout
            write_wait      10s         # Socket write timeout
            shutdown_timeout 10s        # Max graceful shutdown wait

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

By default, WebSocket upgrades accept all origins, matching Reverb's default
`allowed_origins ['*']`. Configure `allowed_origins` to restrict browsers; entries
may be `*`, exact `http://` or `https://` origins, or host-only values such as
`app.example.com`.

Handshake throttling is applied per direct remote IP address. If FrankenPHP sits
behind a reverse proxy or load balancer that hides client IPs, enforce per-client
rate limits at that proxy layer as well.

Private and presence channel auth accepts standard Pusher signatures from
Laravel's `/broadcasting/auth` endpoint:
`socket_id:channel` for private channels and
`socket_id:channel:channel_data` for presence channels. If `auth_script` is
configured and a client omits `auth`, the module falls back to a FrankenPHP auth
worker and validates the worker's returned signature before subscribing the
client.

The native publish functions return `0` on success. Nonzero status codes indicate:
`1` hub missing, `2` channel too long, `3` event too long, `4` payload too large,
`5` invalid payload JSON, `6` broker publish failed, and `7` invalid multi-channel
JSON, `8` broker queue full, and `9` shard queue full. Success means the message
was accepted by the broker and shard queue; delivery to every connected client is
at-most-once and may still fail for slow clients with full outbound queues. The
Laravel `pogo` broadcaster turns native failures into `BroadcastException`.

Fill your .env

```ini
BROADCAST_CONNECTION=pogo
REVERB_APP_ID=pogo-app
REVERB_APP_KEY=change-me-to-a-random-public-app-key
REVERB_APP_SECRET=change-me-to-a-long-random-secret
REVERB_HOST=localhost
REVERB_PORT=8080
REVERB_SCHEME=http
POGO_WEBHOOK_SECRET=change-me-to-a-different-random-secret

VITE_REVERB_APP_KEY="${REVERB_APP_KEY}"
VITE_REVERB_HOST="${REVERB_HOST}"
VITE_REVERB_PORT="${REVERB_PORT}"
VITE_REVERB_SCHEME="${REVERB_SCHEME}"
```

`BROADCAST_CONNECTION=pogo` selects the backend native publish path. The
`VITE_REVERB_*` variables configure Echo's Reverb/Pusher-compatible browser
connection to the same Pogo runtime.

If `app_id`, `app_key`, or `app_secret` are omitted in the Caddyfile, the module
reads `REVERB_APP_ID`, `REVERB_APP_KEY`, and `REVERB_APP_SECRET`.

Start FrankenPHP (`frankenphp` must be compiled with `pogo_websocket`).

```bash
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
| `pogo_websocket_subscriptions_active`          | Gauge     | Active channel subscriptions.                               |
| `pogo_websocket_auth_duration_seconds`         | Histogram | Latency of the PHP Auth Worker.                             |
| `pogo_websocket_client_dropped_messages_total` | Counter   | Messages dropped due to full client buffer.                 |
| `pogo_websocket_publish_failures_total`        | Counter   | Failed publish attempts by app and reason.                  |
| `pogo_websocket_webhook_dropped_total`         | Counter   | Webhook notifications dropped by reason.                    |

## Reliability and security notes

- Redis clustering uses Redis Pub/Sub. Messages are not persisted, replayed, or
  acknowledged across nodes; messages can be lost during Redis outages,
  reconnects, or local overload.
- Laravel's standard `/broadcasting/auth` endpoint signs private and presence
  channel subscriptions. The module validates those Pusher-compatible signatures
  locally before joining the channel.
- Webhook notifications are best-effort and may be dropped when the webhook queue
  is full or the module is shutting down.

---

## Troubleshooting

- **4100 Over Capacity:** Increase `max_connections` in Caddyfile.
- **4009 Connection Unauthorized:** Check `REVERB_APP_KEY` and
  `REVERB_APP_SECRET` match between Laravel and the Caddyfile.
- **Too Many Requests:** Tune `handshake_rate` if legitimate traffic is being blocked.
