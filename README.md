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
`GET /events/ticket` returns a short-lived HMAC-signed ticket — **mount it behind your
auth** so only authenticated users get one. The client connects with `/events?ticket=...`;
a valid ticket unlocks `private-*` subscriptions. Set `REALTIME_SECRET`.


---

## 💎 Premium sponsors

togo is proudly sponsored by **ID8 Media** and **One Studio**.

<p align="center">
  <a href="https://id8media.com"><img src=".github/assets/id8media.svg" height="44" alt="ID8 Media" /></a>
  &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;
  <a href="https://one-studio.co"><img src=".github/assets/one-studio.jpeg" height="44" alt="One Studio" /></a>
</p>
