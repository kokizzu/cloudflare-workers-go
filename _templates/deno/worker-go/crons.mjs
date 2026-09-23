// Cron job definitions for Deno / Deno Deploy.
//
// Deno Deploy discovers cron jobs by evaluating a module's top-level code
// at deployment time, so Deno.cron() calls must live in a top-level JS
// module like this one. workers-assets-gen copies this file into the build
// output next to main.mjs on each build.
//
// Declare each job here, dispatching to the handler registered in Go via
// deno.OnCron(name, handler). Running locally requires the
// --unstable-cron flag (e.g. add it to the "dev" task in deno.json).
//
//   import worker from "./worker.mjs";
//   Deno.cron("my-job", "0 * * * *", () => worker.cron("my-job"));
//
//   // in Go (main.go):
//   //   deno.OnCron("my-job", func() {
//   //       // runs every hour
//   //   })
