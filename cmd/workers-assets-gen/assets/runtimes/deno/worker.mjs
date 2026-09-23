import { run } from "./core.mjs";
import { createRuntimeContext } from "./runtime.mjs";

async function fetch(req, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleRequest(req);
}

// cron handles a Deno Deploy cron invocation. Deno Deploy discovers
// Deno.cron() calls by evaluating top-level module code, so schedules are
// declared in crons.mjs (e.g.
// `Deno.cron("name", "0 * * * *", () => worker.cron("name"))`) and dispatch
// to the handler registered in Go via deno.OnCron(name, handler).
async function cron(name, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  if (binding.runCron === undefined) {
    throw new Error(
      "runCron is not registered: import github.com/syumai/workers-go/exp/deno and call deno.OnCron in Go",
    );
  }
  return binding.runCron(name);
}

export default {
  fetch,
  cron,
};
