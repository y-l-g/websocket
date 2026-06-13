# Changelog

## Unreleased

- Provides a Pusher-compatible WebSocket server as a FrankenPHP/Caddy module.
- Includes a Laravel broadcasting driver.
- Supports private and presence channels, user auth, origin checks, rate limits,
  Redis Pub/Sub clustering, webhooks, and Prometheus metrics.
- Current limits: experimental API and no production replacement claim yet.
- Tightens presence subscription validation, standard Pusher channel signatures,
  bounded overload behavior, context-aware webhook shutdown, and explicit
  at-most-once delivery documentation.
