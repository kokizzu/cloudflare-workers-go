package cloudflare

import (
	"syscall/js"

	"github.com/syumai/workers-go/cloudflare/internal/cfruntimecontext"
	"github.com/syumai/workers-go/internal/jsutil"
)

// WaitUntil extends the lifetime of the "fetch" event.
// It accepts an asynchronous task which the Workers runtime will execute before the handler terminates but without blocking the response.
// see: https://developers.cloudflare.com/workers/runtime-apis/fetch-event/#waituntil
//
// The task runs on a new goroutine and may park and resume (e.g. via
// time.Sleep or channel waits): workers.Done does not close - so the Go
// program does not exit - until every task registered this way has
// returned. Keep tasks bounded; a task that never returns keeps the
// worker instance alive and eventually hits the runtime's own waitUntil
// limit.
func WaitUntil(task func()) {
	exCtx := cfruntimecontext.MustGetExecutionContext()
	exCtx.Call("waitUntil", jsutil.NewPromise(js.FuncOf(func(this js.Value, pArgs []js.Value) any {
		resolve := pArgs[0]
		// The task is tracked as a background task so the program does
		// not exit (workers.Done stays open) while it is still running:
		// resuming a goroutine parked on a timer after the program exited
		// fails under workerd with "Go program has already exited".
		jsutil.TrackBackgroundTask(func() {
			task()
			resolve.Invoke(js.Undefined())
		})
		return js.Undefined()
	})))
}

// PassThroughOnException prevents a runtime error response when the Worker script throws an unhandled exception.
// Instead, the request forwards to the origin server as if it had not gone through the worker.
// see: https://developers.cloudflare.com/workers/runtime-apis/fetch-event/#passthroughonexception
func PassThroughOnException() {
	exCtx := cfruntimecontext.MustGetExecutionContext()
	jsutil.AwaitPromise(jsutil.NewPromise(js.FuncOf(func(this js.Value, pArgs []js.Value) any {
		resolve := pArgs[0]
		go func() {
			exCtx.Call("passThroughOnException")
			resolve.Invoke(js.Undefined())
		}()
		return js.Undefined()
	})))
}
