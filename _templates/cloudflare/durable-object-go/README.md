# durable-object-template-go

- A template for starting a Cloudflare Worker project that hosts a Go type as a [Durable Object](https://developers.cloudflare.com/durable-objects/) class, using [`workers-go`](https://github.com/syumai/workers-go)'s [`exp/cloudflare/durableobjects`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare) package.
- This is a smaller version of the [`durable-object-go` example](https://github.com/syumai/workers-go/tree/main/_examples/durable-object-go): a single `Counter` Durable Object class, fronted by a regular HTTP handler that forwards every request to one well-known instance of it.

## Notice

- `exp/cloudflare` (including `durableobjects`) is experimental: its API may change in a future release. See [exp/cloudflare/README.md](https://github.com/syumai/workers-go/blob/main/exp/cloudflare/README.md), in particular ["Hosting a Go type as a Durable Object"](https://github.com/syumai/workers-go/blob/main/exp/cloudflare/README.md#hosting-a-go-type-as-a-durable-object), for how this works and its current limitations.
- If you don't need Durable Objects, use the plain [worker-go template](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-go) instead.

## Usage

- `main.go` defines the `Counter` Durable Object (`NewCounter`/`Counter.ServeHTTP`) and a regular fetch handler (`handleIndex`) that looks it up via the `COUNTER` binding and forwards every request to it. Feel free to edit this code, add more Durable Object classes, and implement your own logic.
- A Durable Object class must be registered with `durableobjects.Register(className, constructor)` before `workers.Serve` is called, and both `package.json`'s `-durable-objects=` flag and `wrangler.jsonc`'s `durable_objects.bindings[].class_name` must name it the same way. Adding a second Durable Object class means updating all three: register it in `main.go`, add it (comma-separated) to `-durable-objects=` in `package.json`, and add its own binding + `migrations` entry in `wrangler.jsonc`.

## Requirements

- Node.js
- Go 1.24.0 or later

## Getting Started

- Create a new worker project using this template.

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/durable-object-go
```

- Initialize a project.

```console
cd my-app
go mod init
go mod tidy
npm start # start running dev server
curl http://localhost:8787/ # outputs "count=1", then "count=2", ...
```

## Development

### Commands

```
npm start      # run dev server
npm run build  # build Go Wasm binary
npm run deploy # deploy worker
```

### Testing dev server

- Each request to `/` increments and returns the counter persisted by the `Counter` Durable Object instance:

```
$ curl http://localhost:8787/
count=1
$ curl http://localhost:8787/
count=2
```
