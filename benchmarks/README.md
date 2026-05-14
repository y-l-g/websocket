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

* A FrankenPHP binary built with the websocket module for the Pogo run.
* PHP and Composer for each Laravel app.
* Node.js only if frontend assets need to be rebuilt.
* `k6` for the load generator.

## Run

Install each app once:

```bash
cd benchmarks/laravel-broadcast/pogo
composer install
cp .env.example .env
php artisan key:generate
php artisan migrate --force
```

Repeat the same setup in `benchmarks/laravel-broadcast/reverb`.

Run the Pogo app:

```bash
cd benchmarks/laravel-broadcast/pogo
frankenphp run --config Caddyfile
```

Then run the Pogo benchmark:

```bash
cd benchmarks/laravel-broadcast
DRIVER=pogo HOST=localhost HTTP_PORT=80 WS_PORT=80 k6 run benchmark.js
```

Run the Reverb app according to its Laravel/Reverb setup, then run:

```bash
cd benchmarks/laravel-broadcast
DRIVER=reverb HOST=localhost HTTP_PORT=8000 WS_PORT=8080 k6 run benchmark.js
```

The benchmark script expects the Pogo app key to be `pogo-app` and the Reverb app key to be `reverb-key`, matching the checked-in `.env.example` files.

## Fairness Rules

* Use the same machine, PHP version, Laravel version, payload size, and virtual user count.
* Restart each app before a measured run.
* Record the exact command, commit SHA, hardware, and relevant Caddy/Reverb settings with any published result.
* Treat `module/tests/performance/k6_benchmark.js` separately; that script targets the low-level module test fixture, not the Laravel comparison apps.
