# workers-assets-gen

* `workers-assets-gen` command generates files needed to run `workers` package.
  - e.g. wasm_exec.js, worker.mjs ...

## Usage

* See `Makefile` in [templates](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-tinygo).

## Generated layout

The output directory gets the shared core files plus the files owned by the
selected runtime:

* `core.mjs` — the runtime-agnostic Wasm boot protocol (`run()`: module
  instantiation plus `workers.ready` waiting, and the `binding` convention
  the Go side registers its trigger handlers on).
* `wasm_exec.js` — Go/TinyGo glue, selected by `-mode`.
* `runtime.mjs` — the selected runtime's module loader and
  `createRuntimeContext`.
* the runtime's entry file(s) — e.g. `worker.mjs` for Cloudflare, or
  `index.mjs` for Neon.

Runtime assets live under `assets/runtimes/<name>/` and each runtime owns
its entry file and triggers — Cloudflare's worker exports
`fetch/scheduled/queue/email/tail/onRequest` and the `GoDurableObject` /
`GoWorkflowEntrypoint` / `GoWorkerEntrypoint` base classes, Deno exports
`fetch` and `cron` (plus `main.mjs`/`crons.mjs`), and browser/Neon export
`fetch` only.

## Supported options

* `-mode`
  - switch generated file depends on Go / TinyGo.
* `-runtime`
  - select the target runtime (`cloudflare`, `browser`, `deno`, or `neon`; default: `cloudflare`).
  - `deno` also generates `main.mjs`, an entry point that serves the worker via `Deno.serve`.
* `-o`
  - change output directory (default: `build`)
* `-durable-objects` (Cloudflare runtime only)
  - comma-separated list of Durable Object class names (e.g.
    `-durable-objects=Counter,Room`) to define in the generated `worker.mjs`.
    For each name, a subclass of `GoDurableObject` is appended:
    ```js
    export class Counter extends GoDurableObject { static goClassName = "Counter"; }
    ```
    Each name must match the `class_name` used in `wrangler.toml`'s
    `[[durable_objects.bindings]]` (and the corresponding
    `new_classes`/`new_sqlite_classes` migration entry), and the `className`
    passed to `durableobjects.Register` on the Go side (see
    `exp/cloudflare/durableobjects`). Omitting the flag leaves `worker.mjs`
    without any Durable Object class, as before.
* `-workflows` (Cloudflare runtime only)
  - comma-separated list of [Workflow](https://developers.cloudflare.com/workflows/)
    class names (e.g. `-workflows=MyWorkflow,Other`) to define in the
    generated `worker.mjs`. For each name, a subclass of
    `GoWorkflowEntrypoint` is appended:
    ```js
    export class MyWorkflow extends GoWorkflowEntrypoint { static goClassName = "MyWorkflow"; }
    ```
    Each name must match the `class_name` used in `wrangler.toml`'s
    `[[workflows]]`, and the `className` passed to `workflows.Register` on
    the Go side (see `exp/cloudflare/workflows`). Omitting the flag leaves
    `worker.mjs` without any Workflow class, as before.
* `-entrypoints` (Cloudflare runtime only)
  - semicolon-separated list of [Workers RPC](https://developers.cloudflare.com/workers/runtime-apis/rpc/)
    `WorkerEntrypoint` specs (e.g. `-entrypoints=MyService:add,greet;Other`)
    to define in the generated `worker.mjs`. Each spec is either `Name`
    (no RPC methods — just the always-generated `fetch()`) or
    `Name:method1,method2,...` (comma-separated RPC method names). For each
    spec, a subclass of `GoWorkerEntrypoint` is appended:
    ```js
    export class MyService extends GoWorkerEntrypoint {
      static goClassName = "MyService";
      async add(...args) { return (await this._bind()).handleRPC("add", args); }
      async greet(...args) { return (await this._bind()).handleRPC("greet", args); }
      async fetch(req) { return (await this._bind()).handleEntrypointFetch(req); }
    }
    ```
    Each name must match the `entrypoint` used in `wrangler.toml`'s
    `[[services]]` (when reached via a Service binding), and the `className`
    passed to `rpc.Register`/`rpc.RegisterFetch` on the Go side (see
    `exp/cloudflare/rpc`). Omitting the flag leaves `worker.mjs` without any
    WorkerEntrypoint class, as before.

## Future plans

* Runtime subcommands — moving from a flat flag set to
  `workers-assets-gen <runtime> ...` would let each runtime grow its own
  options (like the Cloudflare-only class flags above) without polluting
  the shared flag namespace. `-runtime` could stay as an alias.
* Runtime-neutral env access on the Go side — the Deno runtime currently
  exposes `Deno.env` through a proxy shaped like Cloudflare's `env` object
  so that `cloudflare.Getenv` keeps working. A runtime-neutral accessor
  (e.g. a shared `env` package) would let non-Cloudflare runtimes drop the
  Cloudflare-shaped context keys entirely.
