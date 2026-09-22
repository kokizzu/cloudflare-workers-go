//go:build js && wasm

package workers

import (
	"fmt"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

var (
	httpHandler http.Handler
	doneCh      = make(chan struct{})
	doneOnce    sync.Once
)

func init() {
	jsutil.RegisterAsyncHandler("handleRequest", 1, func(args []js.Value) (js.Value, error) {
		return handleRequest(args[0])
	})
}

// handleRequest accepts a Request object and returns Response object.
func handleRequest(reqObj js.Value) (js.Value, error) {
	if httpHandler == nil {
		return js.Value{}, fmt.Errorf("Serve must be called before handleRequest.")
	}
	return jshttp.ServeRequest(httpHandler, reqObj, func() {
		doneOnce.Do(func() { close(doneCh) })
	})
}

// Serve serves http.Handler on a JS runtime.
// if the given handler is nil, http.DefaultServeMux will be used.
func Serve(handler http.Handler) {
	ServeNonBlock(handler)
	Ready()
	<-Done()
}

// ServeNonBlock sets the http.Handler to be served but does not signal readiness or block
// indefinitely. The non-blocking form is meant to be used in conjunction with Ready and WaitForCompletion.
func ServeNonBlock(handler http.Handler) {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	httpHandler = handler
}

//go:wasmimport workers ready
func ready()

// Ready must be called after all setups of the Go side's handlers are done.
func Ready() {
	ready()
}

// Done returns a channel which is closed when the handler is done.
func Done() <-chan struct{} {
	return doneCh
}
