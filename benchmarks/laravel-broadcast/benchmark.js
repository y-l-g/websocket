import ws from "k6/ws";
import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

const DRIVER = __ENV.DRIVER || "reverb";
const HOST = __ENV.HOST || "localhost";
const HTTP_HOST = __ENV.HTTP_HOST || HOST;
const WS_HOST = __ENV.WS_HOST || HOST;
const HTTP_PORT = __ENV.HTTP_PORT || "8000";
const WS_PORT = __ENV.WS_PORT || (DRIVER === "reverb" ? "8080" : "80");
const APP_KEY = __ENV.APP_KEY || (DRIVER === "reverb" ? "reverb-key" : "pogo-app");
const RESULT_FILE = __ENV.RESULT_FILE || `/results/${DRIVER}-summary.json`;

const VUS = parseInt(__ENV.VUS || "500", 10);
const MSG_COUNT = parseInt(__ENV.MSG_COUNT || "100", 10);
const PAYLOAD_SIZE = parseInt(__ENV.PAYLOAD_SIZE || "1024", 10);
const PUBLISH_BATCHES = parseInt(__ENV.PUBLISH_BATCHES || "20", 10);
const BATCH_INTERVAL_SECONDS = parseFloat(__ENV.BATCH_INTERVAL_SECONDS || "2");
const RAMP_UP_SECONDS = parseInt(__ENV.RAMP_UP_SECONDS || "10", 10);
const HOLD_SECONDS = parseInt(__ENV.HOLD_SECONDS || "45", 10);
const RAMP_DOWN_SECONDS = parseInt(__ENV.RAMP_DOWN_SECONDS || "5", 10);
const PUBLISH_START_SECONDS = parseInt(__ENV.PUBLISH_START_SECONDS || "12", 10);
const PUBLISH_MAX_DURATION_SECONDS = parseInt(
  __ENV.PUBLISH_MAX_DURATION_SECONDS ||
    String(Math.ceil(PUBLISH_BATCHES * BATCH_INTERVAL_SECONDS + 60)),
  10
);

const wsUrl = `ws://${WS_HOST}:${WS_PORT}/app/${APP_KEY}?protocol=7&client=js&version=8.4.0&flash=false`;

const msgLatencyMs = new Trend("msg_latency_ms");
const publishDurationMs = new Trend("publish_duration_ms");
const msgsReceived = new Counter("msgs_received");
const subscriptionsSucceeded = new Counter("subscriptions_succeeded");
const connErrors = new Counter("conn_errors");
const parseErrors = new Counter("parse_errors");
const publishSuccess = new Rate("publish_success");

export const options = {
  summaryTrendStats: ["avg", "min", "med", "max", "p(90)", "p(95)", "p(99)"],
  thresholds: {
    http_req_failed: ["rate==0"],
    publish_success: ["rate==1"],
    ws_sessions: [`count==${VUS}`],
    subscriptions_succeeded: [`count==${VUS}`],
    conn_errors: ["count==0"],
    parse_errors: ["count==0"],
  },
  scenarios: {
    listeners: {
      executor: "ramping-vus",
      stages: [
        { duration: `${RAMP_UP_SECONDS}s`, target: VUS },
        { duration: `${HOLD_SECONDS}s`, target: VUS },
        { duration: `${RAMP_DOWN_SECONDS}s`, target: 0 },
      ],
      gracefulStop: "5s",
      exec: "listener",
    },
    publisher: {
      executor: "shared-iterations",
      vus: 1,
      iterations: PUBLISH_BATCHES,
      startTime: `${PUBLISH_START_SECONDS}s`,
      maxDuration: `${PUBLISH_MAX_DURATION_SECONDS}s`,
      exec: "publisher",
    },
  },
};

export function listener() {
  const res = ws.connect(wsUrl, {}, (socket) => {
    socket.on("open", () => {
      socket.send(JSON.stringify({
        event: "pusher:subscribe",
        data: { channel: "bench-channel" },
      }));
    });

    socket.on("message", (data) => {
      let msg;
      try {
        msg = JSON.parse(data);
      } catch (_) {
        parseErrors.add(1);
        return;
      }

      if (msg.event === "pusher_internal:subscription_succeeded") {
        subscriptionsSucceeded.add(1);
        return;
      }

      if (msg.event !== "bench.event") {
        return;
      }

      try {
        const payload = JSON.parse(msg.data);
        msgLatencyMs.add(Math.max(0, Date.now() - payload.sentAt));
        msgsReceived.add(1);
      } catch (_) {
        parseErrors.add(1);
      }
    });

    socket.on("error", () => connErrors.add(1));
  });

  check(res, { "websocket upgraded": (r) => r && r.status === 101 });
}

export function publisher() {
  const url = `http://${HTTP_HOST}:${HTTP_PORT}/fire?count=${MSG_COUNT}&size=${PAYLOAD_SIZE}`;
  const res = http.get(url, { timeout: `${Math.max(5, BATCH_INTERVAL_SECONDS)}s` });
  const ok = check(res, {
    published: (r) => r.status === 200,
    "published requested count": (r) => {
      try {
        return r.status === 200 && JSON.parse(r.body).count === MSG_COUNT;
      } catch (_) {
        return false;
      }
    },
  });

  publishSuccess.add(ok);
  publishDurationMs.add(res.timings.duration);
  sleep(BATCH_INTERVAL_SECONDS);
}

export function handleSummary(data) {
  const count = (name) => data.metrics[name]?.values.count || 0;
  const percentile = (name, p) => data.metrics[name]?.values[p] ?? null;
  const subscribed = count("subscriptions_succeeded");
  const published = Math.round(count("http_reqs") * (data.metrics.publish_success?.values.rate || 0));
  const observed = count("msgs_received");
  const expected = subscribed * published * MSG_COUNT;
  const missing = Math.max(0, expected - observed);

  const summary = {
    driver: DRIVER,
    generatedAt: new Date().toISOString(),
    config: { vus: VUS, msgCount: MSG_COUNT, payloadSize: PAYLOAD_SIZE, publishBatches: PUBLISH_BATCHES },
    delivery: {
      subscribed,
      published,
      expected,
      observed,
      missing,
      completeness: expected === 0 ? 0 : observed / expected,
    },
    latency: {
      messageP95Ms: percentile("msg_latency_ms", "p(95)"),
      publishP95Ms: percentile("publish_duration_ms", "p(95)"),
    },
    metrics: data.metrics,
  };

  return {
    stdout: [
      "",
      "BENCHMARK SUMMARY",
      `driver=${summary.driver}`,
      `subscribers=${subscribed}`,
      `successful_batches=${published}`,
      `expected_messages=${expected}`,
      `observed_messages=${observed}`,
      `missing_messages=${missing}`,
      `delivery_completeness=${summary.delivery.completeness}`,
      `msg_latency_p95_ms=${summary.latency.messageP95Ms}`,
      `publish_duration_p95_ms=${summary.latency.publishP95Ms}`,
      `summary_file=${RESULT_FILE}`,
      "",
    ].join("\n"),
    [RESULT_FILE]: JSON.stringify(summary, null, 2),
  };
}
