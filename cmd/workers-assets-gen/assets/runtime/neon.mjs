import { readFileSync } from "node:fs";

const modPromise = WebAssembly.compile(
  readFileSync(new URL("./app.wasm", import.meta.url)),
);

export async function loadModule() {
  return await modPromise;
}

// Neon Functions has no Workflows/RPC equivalent, so these are dummies
// (mirroring runtime/browser.mjs) just so worker.mjs's static import of
// WorkflowEntrypointBase/WorkerEntrypointBase (for GoWorkflowEntrypoint,
// and later GoWorkerEntrypoint) resolves.
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

export function createRuntimeContext({ env, ctx, binding }) {
  return {
    env,
    ctx,
    NonRetryableError: undefined,
    binding,
  };
}
