# workers-assets-gen

* `workers-assets-gen` command generates files needed to run `workers` package.
  - e.g. wasm_exec.js, worker.mjs ...

## Usage

* See `Makefile` in [templates](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-tinygo).

## Supported options

* `-mode`
  - switch generated file depends on Go / TinyGo.
* `-runtime`
  - select the target runtime (`cloudflare`, `browser`, `deno`, or `neon`; default: `cloudflare`).
  - `deno` also generates `main.mjs`, an entry point that serves the worker via `Deno.serve`.
* `-o`
  - change output directory (default: `build`)
* `-durable-objects`
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
* `-workflows`
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
* `-entrypoints`
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
