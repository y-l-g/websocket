import ws from "k6/ws";
import { check } from "k6";

export let options = {
  vus: 100, // 100 Virtual Users
  duration: "30s",
};

export default function () {
  const url = "ws://localhost/ws";
  const params = {};

  const res = ws.connect(url, params, function (socket) {
    socket.on("open", function open() {
      // Subscribe to a private channel to trigger PHP Auth
      const payload = JSON.stringify({
        event: "pusher:subscribe",
        data: { channel: "private-user.1" },
      });
      socket.send(payload);
    });

    socket.on("message", function (message) {
      const msg = JSON.parse(message);

      if (msg.event === "pusher_internal:subscription_succeeded") {
        check(msg, { "auth successful": (m) => true });
        // socket.close(); // Keep open to test concurrency
      }

      if (msg.event === "pusher:error") {
        check(msg, { "auth failed": (m) => false });
      }
    });

    socket.on("close", () => console.log("disconnected"));

    // Hold connection for a bit
    socket.setTimeout(function () {
      socket.close();
    }, 5000);
  });

  check(res, { "status is 101": (r) => r && r.status === 101 });
}
