// Cron job definitions for Deno / Deno Deploy.
//
// Deno Deploy discovers cron jobs by evaluating a module's top-level code
// at deployment time, so Deno.cron() calls must live in a top-level JS
// module like this one. Go code (including the deno.Cron binding) only runs
// when a request or job invocation boots the Wasm module — too late to be
// discovered by the deployment-time evaluation.
//
// Declare each job here, dispatching to the handler registered in Go via
// deno.OnCron(name, handler):
//
//   import worker from "./worker.mjs";
//   Deno.cron("my-job", "0 * * * *", () => worker.cron("my-job"));
//
//   // in Go (main.go):
//   //   deno.OnCron("my-job", func() {
//   //       // runs every hour
//   //   })
//
// This file is generated: edit crons.mjs at the project root instead, and
// workers-assets-gen will copy it here on each build.
