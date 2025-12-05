import Echo from "laravel-echo";

import Pusher from "pusher-js";
window.Pusher = Pusher;

window.Echo = new Echo({
  broadcaster: "pusher",
  key: "key",
  cluster: "cluster",
  wsHost: import.meta.env.VITE_FRANKENPHP_HOST || window.location.hostname,
  wsPort: import.meta.env.VITE_FRANKENPHP_PORT || 80,
  wssPort: import.meta.env.VITE_FRANKENPHP_WSS_PORT || 443,
  forceTLS: false,
  disableStats: true,
  enabledTransports: ["ws", "wss"],
});
