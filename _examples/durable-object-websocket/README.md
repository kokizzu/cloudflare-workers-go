# durable-object-websocket

An example of a Cloudflare Workers [Durable Object](https://developers.cloudflare.com/durable-objects/)
hosting a hibernatable [WebSocket](https://developers.cloudflare.com/durable-objects/best-practices/websockets/)
chat room, using
[`exp/cloudflare/durableobjects`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/durableobjects)
and
[`exp/cloudflare/websocket`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/websocket).
Every message a client sends is broadcast to every other client connected
to the same room.

## Running

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Supported commands

```
make dev     # run dev server
make build   # build Go Wasm binary
make deploy  # deploy worker
```

`make build` passes `-durable-objects=Room` to `workers-assets-gen`, the
same way `_examples/durable-object-go` passes `-durable-objects=Counter` —
see `cmd/workers-assets-gen/README.md`.

### Trying it out

1. Start the dev server.
```sh
wrangler dev --local
```

2. Connect two or more WebSocket clients to `ws://localhost:8787/` (e.g.
   with Node 24's global `WebSocket`, or `npx wscat -c ws://localhost:8787/`
   in two terminals) and send a message from one — every connected client,
   including the sender, receives it.

### How it works

* `Room.ServeHTTP` (the Durable Object's `fetch()` trigger) calls
  `websocket.UpgradeHibernating` (not the plain `websocket.Upgrade`
  `_examples/websocket-echo` uses) to build a `*websocket.HibernatingConn`
  without accepting it, then `rm.state.AcceptHibernatingConn(conn, roomTag)`
  to register it for the hibernatable WebSocket API and return immediately —
  there's no read/write loop to start, since messages arrive as separate
  triggers instead of over a channel on the connection value.
* `Room.WebSocketMessage` implements `durableobjects.WebSocketMessageHandler`:
  it's called for every message any connection in this Room sends, looks up
  every connection tagged `roomTag` via `DurableObjectState.GetWebSockets`,
  and calls `WriteMessage` on each.
* `main`'s regular fetch handler (`handleIndex`) forwards every request —
  including the WebSocket upgrade — to the single `Room` instance named
  `"global"`, via `cloudflare.DurableObjectNamespace`/`DurableObjectStub`
  (the same hand-written stub `_examples/durable-object-go` uses for its
  plain HTTP requests). The upgrade specifically needs
  `DurableObjectStub.FetchWebSocket(w, r)` rather than `Fetch`: `Fetch`
  reconstructs a `*http.Response` from the Durable Object's JS `Response`
  (status, headers, body), which has no field for the special
  `ResponseInit.webSocket` pairing a 101 response carries.
  `FetchWebSocket` instead reads that `Response`'s `webSocket` property
  directly and attaches it to `w`'s own response, matching what a plain JS
  Worker gets for free from `return stub.fetch(request)`.
