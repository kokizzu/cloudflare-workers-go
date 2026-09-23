//go:build js && wasm

package deno

import (
	"fmt"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

var cronHandlers = map[string]func(){}

func init() {
	jsutil.RegisterAsyncHandler("runCron", 1, func(args []js.Value) (js.Value, error) {
		name := args[0].String()
		handler, ok := cronHandlers[name]
		if !ok {
			return js.Undefined(), fmt.Errorf("deno: no cron handler registered for %q", name)
		}
		handler()
		return js.Undefined(), nil
	})
}

// OnCron registers handler to be invoked when the cron job named name
// fires. The name must match a Deno.cron declaration in the entry's
// crons.mjs file:
//
//	// crons.mjs (project root; copied next to main.mjs by workers-assets-gen)
//	import worker from "./worker.mjs";
//	Deno.cron("my-job", "0 * * * *", () => worker.cron("my-job"));
//
// Unlike the generated Cron binding (which calls Deno.cron directly from
// Go, and therefore only works under `deno run`), this dispatch works on
// Deno Deploy too: Deploy discovers cron jobs by evaluating the module's
// top-level code at deployment time, so a Deno.cron call inside Go — which
// only runs when a request or job invocation boots the Wasm module — can
// never be discovered. crons.mjs exists so the Deno.cron declarations live
// in real top-level module code while their handlers stay in Go.
func OnCron(name string, handler func()) {
	cronHandlers[name] = handler
}
