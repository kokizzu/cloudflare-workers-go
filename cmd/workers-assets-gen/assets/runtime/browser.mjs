const modPromise = WebAssembly.compileStreaming(fetch("./build/app.wasm"));

export async function loadModule() {
  return await modPromise;
}

// WorkflowEntrypointBase / WorkerEntrypointBase have no real Cloudflare
// Workers counterpart in a browser build; these dummies just let
// worker.mjs's GoWorkflowEntrypoint (and, in the future, GoWorkerEntrypoint)
// classes extend something. Neither class is otherwise usable outside
// runtime/cloudflare.mjs.
export class WorkflowEntrypointBase {
  constructor(ctx, env) {
    this.ctx = ctx;
    this.env = env;
  }
}

export class WorkerEntrypointBase {
  constructor(ctx, env) {
    this.ctx = ctx;
    this.env = env;
  }
}

export function createRuntimeContext({ binding, durableObject, workflow, entrypoint }) {
  return {
    EmailMessage: undefined,
    NonRetryableError: undefined,
    binding,
    durableObject,
    workflow,
    entrypoint,
  };
}
