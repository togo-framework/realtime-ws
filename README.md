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
- **Public** channels: any name (no `private-` prefix), subscribable by anyone.
- **Private** channels: prefix `private-`. Subscribing requires a ticket whose grants
  match the channel — either an exact name or a `prefix*` glob (e.g. a ticket granted
  `private-driver.42*` may subscribe to `private-driver.42.location`).
- Channel names over 200 bytes are rejected with a `subscription_error` frame.
- Broadcast to a channel by emitting `"channel:event"` (e.g. `app.Emit(ctx, "orders:created", o)`).
  A plain event (no `:`) is an **unchanneled broadcast** and only reaches connections that
  presented a valid ticket — anonymous connections never see it.

## Tickets (private channels)
`GET /events/ticket` returns a short-lived (60s), HMAC-signed ticket **scoped to the
channels it grants**. **You mount it, behind your auth**, so only authenticated users
get one:

```go
ws.MountTicket(k, authSvc.Middleware)
```

Tell the package which channels each caller's ticket should grant — this is what makes
a ticket useless for anyone else's private channel:

```go
ws.SetTicketChannels(func(r *http.Request) []string {
	id := auth.Identity(r.Context()) // however your app resolves the caller
	return []string{"private-driver." + id.UserID} // exact names and/or "prefix*" globs
})
```

If `SetTicketChannels` is never called, `IssueTicket` grants **no** private channels
(secure by default) and logs a one-time warning. Call `ws.IssueTicketFor(w, channels)`
directly instead if you'd rather skip `SetTicketChannels` and decide the grants inline.

The client connects with `/events?ticket=...`; a valid ticket unlocks subscriptions to
`private-*` channels it was granted. Set `REALTIME_SECRET` so tickets survive a restart.

The provider mounts no routes of its own, on purpose. A provider that mounts a route
makes chi's mux "have routes", after which any plugin calling `Router.Use` — togo's
auth plugin does, for CORS — panics with *"all middlewares must be defined before
routes on a mux"*. Leaving the mount to the app keeps this package from breaking
another plugin's boot at any priority.

## Origin checking
`websocket.Accept` no longer disables origin verification. Set `REALTIME_ORIGINS` to a
comma-separated list of host patterns (as understood by `coder/websocket`'s
`OriginPatterns`, e.g. `app.example.com,localhost:*`) to allow cross-origin connections.
If unset, only same-host requests are accepted (coder/websocket's default check).

## Per-client buffer
Each connection has a 256-frame outbound buffer, sized for high-frequency streams such
as driver location updates. If a client's reader falls behind and the buffer fills,
further frames for that client are **dropped**, not queued or blocked on — the client
should treat gaps as expected and resync (e.g. re-fetch state) on reconnect.

## Breaking changes in v0.4.0
- **Tickets are now scoped.** The ticket format changed from `nonce.expiry.sig` to a
  base64url JSON payload (`{"c":[channels],"e":expiry,"n":nonce}`) plus `.` and an HMAC
  signature. A valid ticket only unlocks the `private-*` channels it was issued for, not
  every `private-*` channel. Old tickets are incompatible with this version.
- **`IssueTicket` grants nothing by default.** Call `ws.SetTicketChannels(...)` during
  boot, or your users' tickets will not be able to subscribe to any private channel.
- **Unchanneled broadcasts are no longer sent to anonymous connections.** A `Publish`
  call with no `channel:` prefix now reaches only connections that presented a valid
  ticket.
- **`websocket.Accept` no longer sets `InsecureSkipVerify`.** Cross-origin browser
  clients now need `REALTIME_ORIGINS` set, or same-host requests only are accepted.
- **The per-client buffer grew from 32 to 256 frames.** Frames are still dropped, not
  queued, once the buffer is full.
- New exported API: `ws.IssueTicketFor(w, channels)` and `ws.SetTicketChannels(f)`.

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
