# workers-assets-gen

* `workers-assets-gen` command generates files needed to run `workers` package.
  - e.g. wasm_exec.js, worker.mjs ...

## Usage

* See `Makefile` in [templates](https://github.com/syumai/workers-go/tree/main/_templates/cloudflare/worker-tinygo).

## Supported options

* `-mode`
  - switch generated file depends on Go / TinyGo.
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
