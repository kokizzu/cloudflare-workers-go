package hono

import (
	"fmt"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

type Middleware func(c *Context, next func())

var middleware Middleware

func ChainMiddlewares(middlewares ...Middleware) Middleware {
	if len(middlewares) == 0 {
		return nil
	}
	if len(middlewares) == 1 {
		return middlewares[0]
	}
	return func(c *Context, next func()) {
		for i := len(middlewares) - 1; i > 0; i-- {
			i := i
			f := next
			next = func() {
				middlewares[i](c, f)
			}
		}
		middlewares[0](c, next)
	}
}

func init() {
	jsutil.RegisterAsyncHandler("runHonoMiddleware", 1, func(args []js.Value) (js.Value, error) {
		return js.Undefined(), runHonoMiddleware(args[0])
	})
}

func runHonoMiddleware(nextFnObj js.Value) error {
	if middleware == nil {
		return fmt.Errorf("ServeMiddleware must be called before runHonoMiddleware.")
	}
	c := newContext(jsutil.RuntimeContext.Get("ctx"))
	var nextErr error
	next := func() {
		// A rejected next() is captured rather than dropped so that the
		// Middleware signature stays compatible; runHonoMiddleware
		// returns the error after the middleware finishes, which rejects
		// the Promise returned to the JS side.
		if _, err := jsutil.AwaitPromise(nextFnObj.Invoke()); err != nil {
			nextErr = err
		}
	}
	middleware(c, next)
	return nextErr
}

//go:wasmimport workers ready
func ready()

// ServeMiddleware sets the Task to be executed
func ServeMiddleware(middleware_ Middleware) {
	middleware = middleware_
	ready()
	select {}
}
