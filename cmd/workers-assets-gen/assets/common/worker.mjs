import "./wasm_exec.js";
import { createRuntimeContext, loadModule } from "./runtime.mjs";

let mod;

globalThis.tryCatch = (fn) => {
  try {
    return {
      result: fn(),
    };
  } catch (e) {
    return {
      error: e,
    };
  }
};

async function run(ctx) {
  if (mod === undefined) {
    mod = await loadModule();
  }
  const go = new Go();

  let ready;
  const readyPromise = new Promise((resolve) => {
    ready = resolve;
  });
  const instance = new WebAssembly.Instance(mod, {
    ...go.importObject,
    workers: {
      ready: () => {
        ready();
      },
    },
  });
  go.run(instance, ctx);
  await readyPromise;
}

async function fetch(req, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleRequest(req);
}

async function scheduled(event, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.runScheduler(event);
}

async function queue(batch, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleQueueMessageBatch(batch);
}

async function email(message, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleEmail(message);
}

// tail handles a batch of trace events for a Worker configured as a
// tail_consumers entry of another ("producer") Worker.
async function tail(events, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleTail(events);
}

// onRequest handles request to Cloudflare Pages
async function onRequest(ctx) {
  const binding = {};
  const { request, env } = ctx;
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleRequest(request);
}

export default {
  fetch,
  scheduled,
  queue,
  email,
  tail,
  onRequest,
};

// GoDurableObject is the base class a Go-hosted Durable Object subclasses.
// Unlike fetch/scheduled/queue/email above (one wasm instance per trigger
// invocation), a Durable Object instance's whole point is that state (Go
// memory, in-flight goroutines, ...) survives across multiple triggers
// (fetch/alarm/webSocket*) delivered to the same object, so #bind() runs the
// wasm instance's `run()` at most once (memoized in `this.ready`) and reuses
// it for every subsequent trigger.
//
// A subclass is generated per Durable Object class name by
// cmd/workers-assets-gen's -durable-objects flag, e.g.:
//   export class Counter extends GoDurableObject { static goClassName = "Counter"; }
export class GoDurableObject {
  // goClassName identifies this class to the Go side (durableobjects.Register's
  // className argument), via the "durableObject" runtime context entry.
  // A generated subclass sets it explicitly; it falls back to the JS class
  // name (this.constructor.name) if unset.
  static goClassName = undefined;

  constructor(state, env) {
    this.state = state;
    this.env = env;
    this.binding = undefined;
    this.ready = undefined;
  }

  async #bind() {
    if (!this.ready) {
      const binding = {};
      this.binding = binding;
      this.ready = run(
        createRuntimeContext({
          env: this.env,
          ctx: this.state,
          binding,
          durableObject: { className: this.constructor.goClassName ?? this.constructor.name },
        }),
      );
    }
    await this.ready;
    return this.binding;
  }

  async fetch(req) {
    return (await this.#bind()).handleDurableObjectFetch(req);
  }

  async alarm(info) {
    return (await this.#bind()).handleDurableObjectAlarm(info);
  }

  async webSocketMessage(ws, message) {
    return (await this.#bind()).handleDurableObjectWebSocketMessage(ws, message);
  }

  async webSocketClose(ws, code, reason, wasClean) {
    return (await this.#bind()).handleDurableObjectWebSocketClose(ws, code, reason, wasClean);
  }

  async webSocketError(ws, error) {
    return (await this.#bind()).handleDurableObjectWebSocketError(ws, error);
  }
}
