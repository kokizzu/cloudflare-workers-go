import worker from "./worker.mjs";
import "./crons.mjs";

Deno.serve((req) => worker.fetch(req));
