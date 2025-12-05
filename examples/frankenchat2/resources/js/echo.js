import Echo from "laravel-echo";

import Pusher from "pusher-js";
window.Pusher = Pusher;

window.Echo = new Echo({
    broadcaster: "pusher",
    key: "frankenphp-key",
    wsHost: window.location.hostname,
    wsPort: 8000,
    wssPort: 443,
    forceTLS: false,
    disableStats: true,
    enabledTransports: ["ws", "wss"],
    cluster: "mt1",
});
