#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BENCH_DIR="$ROOT_DIR/benchmarks/laravel-broadcast"
COMPOSE_FILE="$BENCH_DIR/compose.yaml"
RESULTS_DIR="$BENCH_DIR/results"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

mkdir -p "$RESULTS_DIR"
chmod 0777 "$RESULTS_DIR"
export K6_UID="${K6_UID:-$(id -u)}"
export K6_GID="${K6_GID:-$(id -g)}"

{
  printf 'timestamp=%s\n' "$STAMP"
  printf 'git_sha=%s\n' "$(git -C "$ROOT_DIR" rev-parse HEAD 2>/dev/null || printf 'unknown')"
  printf 'git_status=\n'
  git -C "$ROOT_DIR" status --short || true
  printf '\nbenchmark_env=\n'
  env | sort | grep -E '^(VUS|MSG_COUNT|PAYLOAD_SIZE|PUBLISH_BATCHES|BATCH_INTERVAL_SECONDS|RAMP_UP_SECONDS|HOLD_SECONDS|RAMP_DOWN_SECONDS|PUBLISH_START_SECONDS|PUBLISH_MAX_DURATION_SECONDS|DRAIN_SECONDS|LATENCY_P95_THRESHOLD_MS|POGO_WS_HOT_PATH_METRICS)=' || true
} > "$RESULTS_DIR/run-$STAMP-meta.txt"

docker compose -f "$COMPOSE_FILE" down -v --remove-orphans
docker compose -f "$COMPOSE_FILE" build --no-cache pogo reverb-app reverb-ws
docker compose -f "$COMPOSE_FILE" images > "$RESULTS_DIR/run-$STAMP-images.txt"

docker compose -f "$COMPOSE_FILE" up --force-recreate --abort-on-container-exit --exit-code-from k6-pogo k6-pogo 2>&1 \
  | tee "$RESULTS_DIR/run-$STAMP-pogo.log"

docker compose -f "$COMPOSE_FILE" down -v --remove-orphans

docker compose -f "$COMPOSE_FILE" up --force-recreate --abort-on-container-exit --exit-code-from k6-reverb k6-reverb 2>&1 \
  | tee "$RESULTS_DIR/run-$STAMP-reverb.log"

docker compose -f "$COMPOSE_FILE" down -v --remove-orphans

printf 'Wrote benchmark results to %s\n' "$RESULTS_DIR"
