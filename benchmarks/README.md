# Benchmarks

Benchmarks in this repository are reproducible measurement harnesses for Pogo WebSocket. They are not examples or showcase applications.

Full product demonstrations belong in `pogoShowcase`. Keep benchmark code here only when it measures websocket behavior or supports performance claims in this repository.

## Laravel Broadcast Scenario

Path: `benchmarks/laravel-broadcast`

This scenario compares two minimal Laravel 12 applications:

* `pogo/`: Laravel broadcasting through `pogo/websocket`.
* `reverb/`: Laravel broadcasting through Laravel Reverb.

Both apps expose the same `/fire?count=...&size=...` endpoint. The k6 script opens websocket clients, subscribes to `bench-channel`, triggers HTTP publishes, and records message delivery and latency metrics.

## Prerequisites

* Docker with Compose.

## Run

Start the Pogo app, then run its k6 workload:

```bash
cd benchmarks/laravel-broadcast
docker compose up -d pogo
docker compose run --rm k6-pogo
```

Start the Reverb HTTP app and websocket server, then run the matching k6 workload:

```bash
cd benchmarks/laravel-broadcast
docker compose up -d reverb-app reverb-ws
docker compose run --rm k6-reverb
```

Clean up containers, networks, and anonymous volumes:

```bash
cd benchmarks/laravel-broadcast
docker compose down -v
```

Compose builds a benchmark-local FrankenPHP image with the websocket, queue, and scheduler modules. It also runs Composer, creates `.env` and the SQLite database, generates an app key when needed, applies migrations, clears Laravel caches, and runs k6 inside containers.

The benchmark script still accepts explicit host and port overrides for advanced runs:

```bash
cd benchmarks/laravel-broadcast
DRIVER=pogo HOST=localhost HTTP_PORT=80 WS_PORT=80 k6 run benchmark.js
DRIVER=reverb HTTP_HOST=localhost WS_HOST=localhost HTTP_PORT=8000 WS_PORT=8080 k6 run benchmark.js
```

The benchmark script expects the Pogo app key to be `pogo-app` and the Reverb app key to be `reverb-key`, matching the checked-in `.env.example` files.

## Fairness Rules

* Use the same machine, PHP version, Laravel version, payload size, and virtual user count.
* Restart each app before a measured run.
* Record the exact command, commit SHA, hardware, and relevant Caddy/Reverb settings with any published result.
* Treat `module/tests/performance/k6_benchmark.js` separately; that script targets the low-level module test fixture, not the Laravel comparison apps.
