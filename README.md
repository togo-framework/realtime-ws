# realtime-ws

WebSocket transport for togo realtime (implements `togo.Broker`). Install it to
upgrade the default SSE broker to WebSockets.

```bash
togo install togo-framework/realtime-ws
```

Clients connect to `/events`; messages are JSON `{ "event": "...", "data": "..." }`.
