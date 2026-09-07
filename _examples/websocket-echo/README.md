# websocket-echo

An example of a Cloudflare Workers WebSocket server using
[`exp/cloudflare/websocket`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/websocket),
which echoes back every message it receives.

## Running

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Supported commands

```
make dev    # run dev server
make build  # build Go Wasm binary
make deploy # deploy worker
```

### Trying it out

1. Start the dev server.
```sh
make dev
```

2. Connect with any WebSocket client, e.g. using `wscat`:
```sh
npx wscat -c ws://localhost:8787/
```

3. Type a message and press enter; the server echoes it back.

### How it works

`main.go`'s handler calls `websocket.Upgrade(w, req)`, then starts a
goroutine that loops on `conn.ReadMessage`/`conn.WriteMessage` and returns
immediately. This is required: Cloudflare only sends the 101 Switching
Protocols response once the handler returns, so the echo loop cannot run
inline before that return.
