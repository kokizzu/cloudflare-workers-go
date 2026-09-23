const wasmUrl = new URL("./app.wasm", import.meta.url);

async function compileModule() {
  if (wasmUrl.protocol === "file:") {
    const bytes = await Deno.readFile(wasmUrl);
    return await WebAssembly.compile(bytes);
  }
  // When modules are served over HTTP instead of the file system, fall back
  // to fetching the Wasm binary.
  return await WebAssembly.compileStreaming(fetch(wasmUrl));
}

const modPromise = compileModule();

export async function loadModule() {
  return await modPromise;
}

// Deno has no Workflows/RPC equivalent, so these are dummies
// (mirroring runtime/neon.mjs) just so worker.mjs's static import of
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

export function createRuntimeContext({ binding }) {
  return {
    // Expose Deno environment variables through the same `env` shape used by
    // the Cloudflare runtime so that cloudflare.Getenv keeps working. The
    // proxy defers Deno.env.get calls, so no --allow-env permission is needed
    // unless a variable is actually read.
    env: new Proxy(
      {},
      {
        get(_target, name) {
          return Deno.env.get(name);
        },
      },
    ),
    binding,
  };
}
