import ws from "k6/ws";
import http from "k6/http";
import { check, sleep } from "k6";
import { Trend, Counter, Rate } from "k6/metrics";

// --- CONFIGURATION ---
const DRIVER = __ENV.DRIVER || "reverb"; // 'reverb' or 'pogo'
const HOST = __ENV.HOST || "localhost";
const HTTP_PORT = __ENV.HTTP_PORT || "8000"; // Laravel Port
const WS_PORT = __ENV.WS_PORT || (DRIVER === "reverb" ? "8080" : "80"); // Reverb default 8080, Pogo default 80
const VUS = __ENV.VUS || 500; // Concurrent Users
const MSG_COUNT = 100; // Messages per batch

// --- METRICS ---
const latencyTrend = new Trend("msg_latency_ms");
const msgCounter = new Counter("msgs_received");
const connErrors = new Counter("conn_errors");
const deliveryRate = new Rate("delivery_success");

// --- URL CONSTRUCTION ---
const appKey = DRIVER === "reverb" ? "reverb-key" : "pogo-app"; // Match your .env
const wsScheme = "ws";
const wsUrl = `${wsScheme}://${HOST}:${WS_PORT}/app/${appKey}?protocol=7&client=js&version=8.4.0&flash=false`;

export const options = {
  scenarios: {
    // 1. Listeners: Maintain connections
    listeners: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: VUS }, // Ramp up
        { duration: "30s", target: VUS }, // Hold
        { duration: "10s", target: 0 }, // Ramp down
      ],
      gracefulStop: "5s",
      exec: "listener",
    },
    // 2. Publisher: Fire events via Laravel HTTP
    publisher: {
      executor: "constant-arrival-rate",
      rate: 1, // 1 batch per second
      timeUnit: "2s",
      duration: "40s",
      preAllocatedVUs: 1,
      startTime: "10s", // Wait for listeners to connect
      exec: "publisher",
    },
  },
};

export function listener() {
  const res = ws.connect(wsUrl, {}, function (socket) {
    socket.on("open", () => {
      // Subscribe to channel
      socket.send(
        JSON.stringify({
          event: "pusher:subscribe",
          data: { channel: "bench-channel" },
        })
      );
    });

    socket.on("message", (data) => {
      const msg = JSON.parse(data);

      // Check for our custom event
      if (msg.event === "bench.event") {
        const payload = JSON.parse(msg.data);
        const now = Date.now();
        // Calculate Latency (Server Sent Time vs Client Recv Time)
        const latency = now - payload.sentAt;

        latencyTrend.add(latency);
        msgCounter.add(1);
        deliveryRate.add(1);
      }
    });

    socket.on("error", (e) => {
      connErrors.add(1);
      console.log("Error: " + e.error());
    });

    socket.on("close", () => console.log("disconnected"));
  });

  check(res, { "status is 101": (r) => r && r.status === 101 });
}

export function publisher() {
  // Tell Laravel to fire events
  const url = `http://${HOST}:${HTTP_PORT}/fire?count=${MSG_COUNT}&size=1024`;
  const res = http.get(url);

  check(res, {
    published: (r) => r.status === 200,
  });
}
