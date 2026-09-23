import worker from "./worker.mjs";

Deno.serve((req) => worker.fetch(req));
