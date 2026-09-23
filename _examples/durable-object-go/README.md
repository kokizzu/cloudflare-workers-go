# durable-object-go

An example of hosting a Go type as a [Durable Object](https://developers.cloudflare.com/durable-objects/)
class, using [`exp/cloudflare/durableobjects`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/durableobjects).

`Counter` (`main.go`) is a Durable Object that persists a request count in
its own `DurableObjectStorage`. The Worker's regular `fetch` handler
(`handleIndex`) forwards every request to the single `Counter` instance
named `"global"`, via the existing hand-written
[`cloudflare.DurableObjectNamespace`](https://github.com/syumai/workers-go/blob/main/cloudflare/dostub.go)/`DurableObjectStub`
— the same way any Worker talks to a Durable Object, whether or not it
hosts that class itself.

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

`make build` passes `-durable-objects=Counter` to `workers-assets-gen`, which
appends the class definition `worker.mjs` needs
(`export class Counter extends GoDurableObject { ... }`) — see
`cmd/workers-assets-gen/README.md`.

### Trying it out

1. Start the dev server.
```sh
wrangler dev
```

2. Hit the Worker a couple of times and watch the count go up:
```sh
curl http://localhost:8787/
curl http://localhost:8787/
```
Both requests are routed to the same `Counter` instance
(`idFromName("global")` always resolves to the same Durable Object ID), so
the second response's count is one higher than the first's — and
`NewCounter`'s log line ("Counter: constructing new instance") only prints
once in the `wrangler dev` output, confirming the same Go instance served
both requests rather than a fresh one per request.

3. Schedule an alarm (fires 5 seconds later; watch the dev server logs for
   "Counter: alarm fired"):
```sh
curl http://localhost:8787/alarm
```
