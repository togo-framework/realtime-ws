# realtime-ws

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
your auth** — `ws.MountTicket(k, authSvc.Middleware)` — so only authenticated users get
one. The client connects with `/events?ticket=...`; a valid ticket unlocks `private-*`
subscriptions. Set `REALTIME_SECRET`.

The provider mounts no routes itself: a provider that mounts a route makes chi's mux
"have routes", and any plugin calling `Router.Use` afterwards (togo's auth plugin does,
for CORS) panics. Since v0.3.0 the mount is the app's job.
