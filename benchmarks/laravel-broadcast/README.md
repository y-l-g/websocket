# Laravel Broadcast Benchmark

Compares Laravel broadcasting through Pogo WebSocket with Laravel Reverb. Each app exposes `/fire?count=...&size=...`; k6 opens websocket clients, subscribes to `bench-channel`, triggers HTTP publishes, and records delivery/latency metrics.

## Run

Requires Docker with Compose.

```bash
POGO_WS_HOT_PATH_METRICS=true ./benchmarks/laravel-broadcast/run.sh
```

The runner performs a `docker compose down -v`, fresh `--no-cache` application image builds, an isolated Pogo run, an isolated Reverb run, and final cleanup. Console logs, image metadata, run metadata, and k6 JSON summaries are written to `benchmarks/laravel-broadcast/results/`.

The default schedule keeps websocket listeners connected until the publisher's configured `maxDuration` plus a drain buffer has elapsed. This prevents late publish batches from being counted as expected delivery after subscribers have already shut down.

For a quick smoke test:

```bash
VUS=20 MSG_COUNT=5 PUBLISH_BATCHES=3 BATCH_INTERVAL_SECONDS=1 PUBLISH_MAX_DURATION_SECONDS=15 DRAIN_SECONDS=5 ./benchmarks/laravel-broadcast/run.sh
```

Manual runs are still possible:

```bash
docker compose -f benchmarks/laravel-broadcast/compose.yaml build --no-cache pogo reverb-app reverb-ws
docker compose -f benchmarks/laravel-broadcast/compose.yaml up --force-recreate --abort-on-container-exit --exit-code-from k6-pogo k6-pogo
docker compose -f benchmarks/laravel-broadcast/compose.yaml down -v
docker compose -f benchmarks/laravel-broadcast/compose.yaml up --force-recreate --abort-on-container-exit --exit-code-from k6-reverb k6-reverb
docker compose -f benchmarks/laravel-broadcast/compose.yaml down -v
```

`benchmark.js` accepts `DRIVER`, `APP_KEY`, `HTTP_HOST`, `WS_HOST`, `HTTP_PORT`, `WS_PORT`, `VUS`, `MSG_COUNT`, `PAYLOAD_SIZE`, `PUBLISH_BATCHES`, `BATCH_INTERVAL_SECONDS`, `RAMP_UP_SECONDS`, `HOLD_SECONDS`, `RAMP_DOWN_SECONDS`, `PUBLISH_START_SECONDS`, `PUBLISH_MAX_DURATION_SECONDS`, `DRAIN_SECONDS`, `LATENCY_P95_THRESHOLD_MS`, and `RESULT_FILE` overrides.

If `HOLD_SECONDS` is not set, the benchmark derives it from `PUBLISH_START_SECONDS + PUBLISH_MAX_DURATION_SECONDS + DRAIN_SECONDS - RAMP_UP_SECONDS`. If `HOLD_SECONDS` is set too low, k6 aborts instead of writing a misleading delivery summary.

The default benchmark intentionally compares the current Pogo integrated FrankenPHP websocket setup with the current Reverb split app/websocket setup. Treat it as a topology benchmark, not an isolated websocket-engine microbenchmark.
