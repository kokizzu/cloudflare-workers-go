# workers-go

[![Go Reference](https://pkg.go.dev/badge/github.com/syumai/workers-go.svg)](https://pkg.go.dev/github.com/syumai/workers-go)
[![Discord Server](https://img.shields.io/discord/1095344956421447741?logo=discord&style=social)](https://discord.gg/tYhtatRqGs)

* `workers-go` is a Go module to run an HTTP server written in Go on [Cloudflare Workers](https://workers.cloudflare.com/). Its root package is `workers`.
* This package can easily serve *http.Handler* on Cloudflare Workers.
* Caution: This is an experimental project.

**Documentation: <https://workers-go.syumai.dev>** — feature support, per-platform guides, the full binding reference, and the FAQ live there.

## Installation

```
go get github.com/syumai/workers-go
```

## Usage

Implement your `http.Handler` and give it to `workers.Serve()`.

```go
func main() {
	http.HandleFunc("/hello", func (w http.ResponseWriter, req *http.Request) { ... })
	workers.Serve(nil) // if nil is given, http.DefaultServeMux is used.
}
```

## Quick Start

* You can easily create and deploy a project from `Deploy to Cloudflare` button.

[![Deploy to Cloudflare](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https%3A%2F%2Fgithub.com%2Fsyumai%2Fworker-go-deploy)

* If you want to create a project manually, please follow the guide below.

### Requirements

* Node.js (and npm)
* Go 1.24.0 or later

### Create a new Worker project

```console
npm create cloudflare@latest -- --template github.com/syumai/workers-go/_templates/cloudflare/worker-go
```

### Initialize the project

1. Navigate to your new project directory and initialize Go modules:

```console
cd my-app
go mod init
go mod tidy
```

2. Start the development server:

```console
npm start
```

3. Verify the worker is running:

```console
curl http://localhost:8787/hello
```

You will see **"Hello!"** as the response.

## Learn more

* [Documentation](https://workers-go.syumai.dev)
  * [Feature support](https://workers-go.syumai.dev/introduction#feature-support) — supported Workers features and their status
  * [Quickstart](https://workers-go.syumai.dev/quickstart) — detailed setup, including the TinyGo template
  * [Cloudflare](https://workers-go.syumai.dev/cloudflare) — bindings for KV, R2, D1, Durable Objects, Queues, Cron, and more
  * [Generated bindings (`exp/cloudflare`)](https://workers-go.syumai.dev/cloudflare/generated-bindings) — bindings auto-generated from `@cloudflare/workers-types`
  * [Deno / Deno Deploy](https://workers-go.syumai.dev/deno) — run the same `http.Handler` via `Deno.serve`
  * [FAQ](https://workers-go.syumai.dev/faq)
* [`_examples`](_examples) — a runnable project for almost every feature
* [`exp/cloudflare`](exp/cloudflare/README.md) — experimental generated bindings and how they are produced
