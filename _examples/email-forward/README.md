# email-forward

An example of a Cloudflare Worker that receives inbound mail via
[Email Routing](https://developers.cloudflare.com/email-routing/email-workers/)
and forwards it to a verified destination address, using
[`exp/cloudflare/email`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/email).

## Development

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.21 or later

### Setup

1. Edit `forwardTo` in `main.go` to a destination address you've already
   verified for Email Routing on this zone (Cloudflare requires proving
   control of an address before Workers may forward mail to it).
2. Deploy the Worker (`make deploy`).
3. In the dashboard, under Email > Email Routing, create a route with a
   "Send to a Worker" action pointing at this Worker. `wrangler.toml` has
   no key for this: the inbound trigger is configured on the zone, not in
   the Worker's own config.

### Commands

```
make dev    # run dev server
make build  # build Go Wasm binary
make deploy # deploy worker
```
