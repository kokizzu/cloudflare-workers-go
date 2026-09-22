# rpc-go

An example of hosting a Go type's methods as a
[Workers RPC](https://developers.cloudflare.com/workers/runtime-apis/rpc/)
`WorkerEntrypoint`, using
[`exp/cloudflare/rpc`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/rpc).

`main.go` registers two RPC methods (`add`, `greet`) and a `fetch()` handler
for the `MyService` entrypoint via `rpc.Register`/`rpc.RegisterFetch`. The
Worker's regular `fetch` handler (`handleIndex`) then calls into that same
entrypoint through the `SELF` Service binding (`wrangler.toml`'s
`[[services]]`, pointing back at this Worker's own `MyService` entrypoint)
using `rpc.NewStub("SELF")` — the same way any other Worker would call into
a Service binding pointing at a WorkerEntrypoint this Worker hosts.

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

`make build` passes `-entrypoints=MyService:add,greet` to
`workers-assets-gen`, which appends the class definition `worker.mjs` needs
(`export class MyService extends GoWorkerEntrypoint { ... }`) — see
`cmd/workers-assets-gen/README.md`.

### Trying it out

1. Start the dev server.
```sh
wrangler dev
```

2. Request `/`:
```sh
curl http://localhost:8787/
# {"sum":3,"greeting":"hello, go"}
```
