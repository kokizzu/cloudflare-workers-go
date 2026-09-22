# tail-worker

An example of a [Tail Worker](https://developers.cloudflare.com/workers/observability/logs/tail-workers/)
that receives batches of trace events from another ("producer") Worker and
logs a summary of each, using
[`exp/cloudflare/tail`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/tail).

## Development

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.21 or later

### Setup

1. Deploy this Worker (`make deploy`).
2. In the *producer* Worker you want to observe, add a `tail_consumers`
   entry to its `wrangler.toml` pointing at this Worker's name (there is no
   key on this Worker's own `wrangler.toml` for the inbound trigger — see
   the comment there):
   ```toml
   tail_consumers = [{ service = "tail-worker" }]
   ```
3. Deploy the producer Worker. Every invocation it handles now also
   delivers a batch of `TraceItem`s to this Worker's `tail(events, env,
   ctx)` handler.

### Commands

```
make dev    # run dev server
make build  # build Go Wasm binary
make deploy # deploy worker
```
