package jsutil

import (
	"errors"
	"fmt"
	"syscall/js"
)

// TryCatch runs fn through globalThis.tryCatch's try/catch and returns
// either its result or a Go error. fn is a plain Go function (not a
// js.Func): it is wrapped here in a js.FuncOf whose callback recovers
// any panic raised by fn - e.g. Value.Invoke panicking when the JS call
// inside fn throws, which is how cloudflare/sockets.Connect surfaces a
// rejected connect(). A panic escaping a js.FuncOf callback in this
// reentrant position (Go -> tryCatch -> fn) is not converted to a JS
// exception and hangs the process, so it must be recovered inside the
// callback itself.
func TryCatch(fn func() js.Value) (js.Value, error) {
	panicCh := make(chan any, 1)
	wrapped := js.FuncOf(func(js.Value, []js.Value) (ret any) {
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
				ret = js.Undefined()
			}
		}()
		return fn()
	})
	defer wrapped.Release()

	fnResultVal := js.Global().Call("tryCatch", wrapped)
	select {
	case r := <-panicCh:
		return js.Value{}, fmt.Errorf("panic: %v", r)
	default:
	}
	resultVal := fnResultVal.Get("result")
	errorVal := fnResultVal.Get("error")
	if !errorVal.IsUndefined() {
		return js.Value{}, errors.New(errorVal.String())
	}
	return resultVal, nil
}
