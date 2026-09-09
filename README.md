<!-- togo-header -->
<div align="center">
  <img src=".github/assets/togo-mark.svg" alt="togo" height="64" />
  <h1>togo-framework/realtime-ws</h1>
  <p>
    <a href="https://to-go.dev/marketplace"><img src="https://img.shields.io/badge/marketplace-to--go.dev-1FC7DC" alt="marketplace" /></a>
    <a href="https://pkg.go.dev/github.com/togo-framework/realtime-ws"><img src="https://pkg.go.dev/badge/github.com/togo-framework/realtime-ws.svg" alt="pkg.go.dev" /></a>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" />
  </p>
  <p><strong>Part of the <a href="https://to-go.dev">togo</a> framework.</strong></p>
</div>

## Install

```bash
togo install togo-framework/realtime-ws
```

<!-- /togo-header -->

<!-- togo-brand -->
<p align="center">
  <img src=".github/assets/togo-mark.svg" width="96" alt="togo" />
</p>
<h1 align="center">realtime-ws</h1>
<p align="center"><sub>part of the <a href="https://github.com/togo-framework">togo-framework</a> — the full-stack Go + React framework</sub></p>

Channel-based WebSocket transport for togo realtime (implements `togo.Broker`,
overrides the default SSE broker when installed).

```bash
togo install togo-framework/realtime-ws
```

## Channels & broadcasting
- Clients subscribe over the socket: `{"action":"subscribe","channel":"orders"}`.
- **Public** channels: any name. **Private** channels: prefix `private-` (require a ticket).
- Broadcast to a channel by emitting `"channel:event"` (e.g. `app.Emit(ctx, "orders:created", o)`);
  a plain event (no `:`) goes to everyone.

## Tickets (private channels)
`GET /events/ticket` returns a short-lived HMAC-signed ticket. **You mount it, behind
your auth**, so only authenticated users get one:

```go
ws.MountTicket(k, authSvc.Middleware)
```

The client connects with `/events?ticket=...`; a valid ticket unlocks `private-*`
subscriptions. Set `REALTIME_SECRET` so tickets survive a restart.

The provider mounts no routes of its own, on purpose. A provider that mounts a route
makes chi's mux "have routes", after which any plugin calling `Router.Use` — togo's
auth plugin does, for CORS — panics with *"all middlewares must be defined before
routes on a mux"*. Leaving the mount to the app keeps this package from breaking
another plugin's boot at any priority.

> **Upgrading from v0.2.0** — `/events/ticket` was mounted for you (unauthenticated,
> and it made any app using togo auth panic on boot). Call `ws.MountTicket` yourself
> now. `IssueTicket` is exported if you would rather mount it by hand.


---

## 💎 Premium sponsors

togo is proudly sponsored by **ID8 Media** and **One Studio**.

<p align="center">
  <a href="https://id8media.com"><img src=".github/assets/id8media.svg" height="44" alt="ID8 Media" /></a>
  &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
  <a href="https://one-studio.co"><img src=".github/assets/one-studio.jpeg" height="44" alt="One Studio" /></a>
</p>

<!-- togo-sponsors -->
---

<div align="center">
  <h3>Premium sponsors</h3>
  <p>
    <a href="https://id8media.com"><strong>ID8 Media</strong></a> &nbsp;·&nbsp;
    <a href="https://one-studio.co"><strong>One Studio</strong></a>
  </p>
  <p><sub>Support togo — <a href="https://github.com/sponsors/fadymondy">become a sponsor</a>.</sub></p>
</div>
<!-- /togo-sponsors -->
