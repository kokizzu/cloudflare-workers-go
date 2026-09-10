# workers-go

[![Go Reference](https://pkg.go.dev/badge/github.com/syumai/workers-go.svg)](https://pkg.go.dev/github.com/syumai/workers-go)
[![Discord Server](https://img.shields.io/discord/1095344956421447741?logo=discord&style=social)](https://discord.gg/tYhtatRqGs)

* `workers-go` is a Go module to run an HTTP server written in Go on [Cloudflare Workers](https://workers.cloudflare.com/). Its root package is `workers`.
* This package can easily serve *http.Handler* on Cloudflare Workers.
* Caution: This is an experimental project.

## Features

* [x] serve http.Handler
* [ ] R2
  - [x] Head
  - [x] Get
  - [x] Put
  - [x] Delete
  - [x] List
  - [ ] Options for R2 methods
* [ ] KV
  - [x] Get
  - [x] List
  - [x] Put
  - [x] Delete
  - [ ] Options for KV methods
* [x] Cache API
* [ ] Durable Objects
  - [x] Calling stubs
* [x] D1 (alpha)
* [x] Environment variables
* [x] FetchEvent
* [x] Cron Triggers
* [x] TCP Sockets
* [x] Queues
  - [x] Producer
  - [x] Consumer
* [x] Additional runtime bindings and triggers, auto-generated from `@cloudflare/workers-types` — see [Experimental packages (`exp/cloudflare`)](#experimental-packages-expcloudflare) below

## Experimental packages (`exp/cloudflare`)

In addition to the stable packages above, [`exp/cloudflare`](exp/cloudflare/README.md) provides Go bindings for more Cloudflare Workers runtime APIs, mostly auto-generated from `@cloudflare/workers-types`. These packages are experimental: their API may still change in a future release.

| Package | Description |
| --- | --- |
| [`ratelimit`](exp/cloudflare/ratelimit) | Rate Limiting binding |
| [`versions`](exp/cloudflare/versions) | Worker Version metadata (`version_metadata` binding) |
| [`analytics`](exp/cloudflare/analytics) | Analytics Engine binding |
| [`hyperdrive`](exp/cloudflare/hyperdrive) | Hyperdrive binding, including a `net.Conn` bridge into `cloudflare/sockets` |
| [`workflows`](exp/cloudflare/workflows) | Workflows binding |
| [`vectorize`](exp/cloudflare/vectorize) | Vectorize (vector database) binding |
| [`cf`](exp/cloudflare/cf) | `request.cf`, the metadata Cloudflare's edge attaches to a request |
| [`kv`](exp/cloudflare/kv) | Generated Level 1 Workers KV binding, wrapped by the stable `cloudflare/kv` package above |
| [`r2`](exp/cloudflare/r2) | Generated Level 1 R2 binding, wrapped by the stable `cloudflare/r2` package |
| [`queues`](exp/cloudflare/queues) | Generated Level 1 Queues producer binding, wrapped by the stable `cloudflare/queues` package |
| [`cache`](exp/cloudflare/cache) | Generated Level 1 Cache API binding, wrapped by the stable `cache` package |
| [`images`](exp/cloudflare/images) | Cloudflare Images runtime (image transformation) binding |
| [`ai`](exp/cloudflare/ai) | Workers AI binding |
| [`email`](exp/cloudflare/email) | Email Workers binding: sending, forwarding, and the `email(message, env, ctx)` handler |
| [`websocket`](exp/cloudflare/websocket) | Server-side WebSocket upgrade, including Durable Object hibernation support |
| [`durableobjects`](exp/cloudflare/durableobjects) | Durable Objects: generated bindings plus hand-written hosting support for writing a Durable Object class itself in Go — see [Hosting a Go type as a Durable Object](exp/cloudflare/README.md#hosting-a-go-type-as-a-durable-object) |
| [`tail`](exp/cloudflare/tail) | Tail Worker support: the `tail(events, env, ctx)` handler for consuming another Worker's execution traces |
| [`secrets`](exp/cloudflare/secrets) | Secrets Store binding |
| [`dispatch`](exp/cloudflare/dispatch) | Workers for Platforms dynamic dispatch namespace binding |

See [exp/cloudflare/README.md](exp/cloudflare/README.md) for how these packages are generated, their type-mapping conventions, and the guide to hosting a Go type as a Durable Object (also available as the [`durable-object-go` template](_templates/cloudflare/durable-object-go)).

## Installation

```
go get github.com/syumai/workers-go
```

### Migrating from `github.com/syumai/workers`

This module was published as `github.com/syumai/workers` up to v0.34.0 and was renamed to `github.com/syumai/workers-go` in v0.35.0 ([#173](https://github.com/syumai/workers-go/issues/173)). The old path still works: it is now a thin forwarding module that re-exports this module's API and follows its releases for a transition period, but it is marked deprecated. To switch, rewrite the import paths and tidy:

```
# Linux
find . \( -name '*.go' -o -name go.mod \) -exec sed -i 's|github.com/syumai/workers\([/" ]\)|github.com/syumai/workers-go\1|g' {} +
# macOS
find . \( -name '*.go' -o -name go.mod \) -exec sed -i '' 's|github.com/syumai/workers\([/" ]\)|github.com/syumai/workers-go\1|g' {} +
go mod tidy
```

## Usage

implement your http.Handler and give it to `workers.Serve()`.

```go
func main() {
	var handler http.HandlerFunc = func (w http.ResponseWriter, req *http.Request) { ... }
	workers.Serve(handler)
}
```

or just call `http.Handle` and `http.HandleFunc`, then invoke `workers.Serve()` with nil.

```go
func main() {
	http.HandleFunc("/hello", func (w http.ResponseWriter, req *http.Request) { ... })
	workers.Serve(nil) // if nil is given, http.DefaultServeMux is used.
}
```

For concrete examples, see `_examples` directory.

## Quick Start

* You can easily create and deploy a project from `Deploy to Cloudflare` button.

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https%3A%2F%2Fgithub.com%2Fsyumai%2Fworker-go-deploy)

* If you want to create a project manually, please follow the guide below.

### Requirements

* Node.js (and npm)
* Go 1.24.0 or later

### Create a new Worker project

Run the following command:

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/worker-go
```

After creating the project, follow the steps below to initialize it.

### Initialize the project

1. Navigate to your new project directory:

```console
cd my-app
```

2. Initialize Go modules:

```console
go mod init
go mod tidy
```

3. Start the development server:

```console
npm start
```

4. Verify the worker is running:

```console
curl http://localhost:8787/hello
```

You will see **"Hello!"** as the response.

If you want a more detailed description, please refer to the README.md file in the generated directory.

## FAQ

### How do I deploy a worker implemented in this package?

To deploy a Worker, the following steps are required.

* Create a worker project using [wrangler](https://developers.cloudflare.com/workers/wrangler/).
* Build a Wasm binary.
* Upload a Wasm binary with a JavaScript code to load and instantiate Wasm (for entry point).

The [worker-go template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-go) contains all the required files, so I recommend using this template.

If you want a smaller Wasm binary, you can use the [TinyGo template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-tinygo) instead.

The TinyGo template requires TinyGo 0.42.0 or later. TinyGo 0.41.x cannot build `net/http` for Wasm (see [tinygo-org/tinygo#5350](https://github.com/tinygo-org/tinygo/issues/5350)).

If you want to host a Go type as a [Durable Object](https://developers.cloudflare.com/durable-objects/), use the [`durable-object-go` template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/durable-object-go) instead — see [Experimental packages (`exp/cloudflare`)](#experimental-packages-expcloudflare) above.

### Where can I have discussions about contributions, or ask questions about how to use the library?

You can do both through GitHub Issues. If you want to have a more casual conversation, please use the [Discord server](https://discord.gg/tYhtatRqGs).
