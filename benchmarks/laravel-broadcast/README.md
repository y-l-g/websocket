# Laravel Broadcast Benchmark

Compares Laravel broadcasting through Pogo WebSocket with Laravel Reverb. Each app exposes `/fire?count=...&size=...`; k6 opens websocket clients, subscribes to `bench-channel`, triggers HTTP publishes, and records delivery/latency metrics.

## Run

Requires Docker with Compose.

```bash
docker compose up --build k6-pogo
docker compose up --build k6-reverb
docker compose down -v
```

`benchmark.js` accepts `DRIVER`, `HTTP_HOST`, `WS_HOST`, `HTTP_PORT`, `WS_PORT`, and `VUS` overrides for custom runs.
