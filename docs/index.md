# realtime-ws

Channel-based WebSocket transport for togo realtime (implements `togo.Broker`,
overrides the default SSE broker when installed).

```bash
togo install togo-framework/realtime-ws
```

## Channels & broadcasting
- Clients subscribe over the socket: `{"action":"subscribe","channel":"orders"}`.
- **Public** channels: any name (no `private-` prefix). **Private** channels: prefix
  `private-`, and require a ticket grant that matches — an exact name or a `prefix*`
  glob. Channel names over 200 bytes are rejected.
- Broadcast to a channel by emitting `"channel:event"` (e.g. `app.Emit(ctx, "orders:created", o)`).
  A plain event (no `:`) is an unchanneled broadcast, delivered only to connections that
  presented a valid ticket — never to anonymous connections.

## Tickets (private channels)
`GET /events/ticket` returns a short-lived (60s) HMAC-signed ticket scoped to the
channels it grants. **You mount it, behind your auth** —
`ws.MountTicket(k, authSvc.Middleware)` — so only authenticated users get one, and you
register `ws.SetTicketChannels(func(r *http.Request) []string { ... })` so each caller's
ticket is scoped to their own channels. Without `SetTicketChannels`, tickets grant no
private channels. The client connects with `/events?ticket=...`. Set `REALTIME_SECRET`.

## Origin & buffering
`REALTIME_ORIGINS` (comma-separated host patterns) allows cross-origin connections;
unset means same-host only. Each client has a 256-frame outbound buffer; a full buffer
drops new frames rather than blocking.

The provider mounts no routes itself: a provider that mounts a route makes chi's mux
"have routes", and any plugin calling `Router.Use` afterwards (togo's auth plugin does,
for CORS) panics. Since v0.3.0 the mount is the app's job.

## v0.4.0
Tickets are now channel-scoped (a ticket for `private-a` cannot subscribe to
`private-b`), unchanneled broadcasts no longer reach anonymous connections, Origin is
verified by default, and the per-client buffer grew to 256 frames. See the README's
"Breaking changes in v0.4.0" section.
