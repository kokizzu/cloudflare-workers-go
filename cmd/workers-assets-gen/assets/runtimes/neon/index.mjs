import { run } from "./core.mjs";
import { createRuntimeContext } from "./runtime.mjs";

async function fetch(req, env, ctx) {
  const binding = {};
  await run(createRuntimeContext({ env, ctx, binding }));
  return binding.handleRequest(req);
}

export default {
  fetch,
};
