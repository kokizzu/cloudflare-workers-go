package jsutil

import "syscall/js"

// RegisterAsyncHandler registers a JS-callable function named name on the
// Go binding object (jsutil.Binding). The registered function returns a
// Promise. The handler runs in a new goroutine; a non-nil error rejects the
// Promise, otherwise it resolves with the returned value (js.Undefined() if nil).
// If more than maxArgs arguments are given, the Promise is rejected.
func RegisterAsyncHandler(name string, maxArgs int, handler func(args []js.Value) (js.Value, error)) {
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				// A panic that escapes handler must not be allowed to
				// propagate out of this goroutine: an unrecovered panic
				// terminates the whole wasm program without ever calling
				// resolve/reject, leaving the JS side's Promise pending
				// forever instead of surfacing the failure. Converting it
				// into a rejection keeps the Promise contract intact and
				// lets the JS caller observe the error.
				defer func() {
					if r := recover(); r != nil {
						reject.Invoke(Errorf("panic in %s handler: %v", name, r))
					}
				}()
				if len(args) > maxArgs {
					reject.Invoke(Errorf("too many args given to %s: %d", name, len(args)))
					return
				}
				result, err := handler(args)
				if err != nil {
					reject.Invoke(Error(err.Error()))
					return
				}
				resolve.Invoke(result)
			}()
			return js.Undefined()
		})
		return NewPromise(cb)
	})
	Binding.Set(name, callback)
}

// AsyncFunc is RegisterAsyncHandler's Binding-independent core: it builds a
// JS-callable js.Func (rather than registering it as a named entry on
// jsutil.Binding) that returns a Promise when invoked from JS. The handler
// runs in a new goroutine; a non-nil error rejects the Promise, otherwise it
// resolves with the returned value (js.Undefined() if the value is the zero
// js.Value). The caller is responsible for Release-ing the returned js.Func
// once it is no longer needed (e.g. after the JS call it was passed to has
// settled).
//
// This is used by generated bindings for a method whose only parameter is a
// callback of TypeScript shape "(a: A) => Promise<U>" or "() => Promise<U>"
// (e.g. DurableObjectStorage.transaction, DurableObjectState.
// blockConcurrencyWhile) — see exp/internal/jsrt.AsyncFunc and
// tmp/06-codegen-spec.md 5.1 item 5.
func AsyncFunc(handler func(args []js.Value) (js.Value, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				// See the matching comment in RegisterAsyncHandler: without
				// this, a panic in handler would crash the wasm program
				// instead of rejecting the Promise.
				defer func() {
					if r := recover(); r != nil {
						reject.Invoke(Errorf("panic in async handler: %v", r))
					}
				}()
				result, err := handler(args)
				if err != nil {
					reject.Invoke(Error(err.Error()))
					return
				}
				resolve.Invoke(result)
			}()
			return js.Undefined()
		})
		return NewPromise(cb)
	})
}
