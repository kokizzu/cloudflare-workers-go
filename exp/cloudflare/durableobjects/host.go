//go:build js && wasm

package durableobjects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// Object is a Go implementation of a Durable Object class. Its ServeHTTP
// handles the object's fetch() trigger; a *Constructor's Object may also
// implement AlarmHandler, WebSocketMessageHandler, WebSocketCloseHandler,
// and/or WebSocketErrorHandler to receive the corresponding triggers — each
// is dispatched only if implemented, per tmp/06-codegen-spec.md 4.2.
type Object interface {
	http.Handler
}

// AlarmHandler is implemented by an Object that wants to receive the
// Durable Object's alarm() trigger (see DurableObjectStorage.SetAlarm).
// info is nil when the runtime does not provide invocation metadata.
type AlarmHandler interface {
	Alarm(ctx context.Context, info *AlarmInvocationInfo) error
}

// WebSocketMessageHandler is implemented by an Object that wants to receive
// the Durable Object's webSocketMessage() trigger, delivered for a
// WebSocket accepted via DurableObjectState.AcceptWebSocket (the
// hibernatable WebSocket API). ws is the raw JS WebSocket value; message is
// either a JS string or an ArrayBuffer, matching the JS side's
// `string | ArrayBuffer` union — decode it yourself (e.g.
// message.Type() == js.TypeString) depending on what you send.
type WebSocketMessageHandler interface {
	WebSocketMessage(ctx context.Context, ws js.Value, message js.Value) error
}

// WebSocketCloseHandler is implemented by an Object that wants to receive
// the Durable Object's webSocketClose() trigger.
type WebSocketCloseHandler interface {
	WebSocketClose(ctx context.Context, ws js.Value, code int, reason string, wasClean bool) error
}

// WebSocketErrorHandler is implemented by an Object that wants to receive
// the Durable Object's webSocketError() trigger.
type WebSocketErrorHandler interface {
	WebSocketError(ctx context.Context, ws js.Value, err error) error
}

// Constructor builds an Object for one Durable Object instance, given its
// DurableObjectState (ctx) and environment bindings (env). It is called at
// most once per wasm instance (i.e. once per Durable Object instance's
// lifetime; see the package doc comment on the 1-instance-per-object model),
// the first time any trigger reaches this instance.
type Constructor func(state *DurableObjectState, env js.Value) (Object, error)

var (
	constructorsMu sync.Mutex
	constructors   = map[string]Constructor{}
)

// Register associates className (the Durable Object class name — matching
// both the -durable-objects flag passed to workers-assets-gen and
// wrangler.toml's [[durable_objects.bindings]] class_name) with ctor.
// Register must be called for every Durable Object class this Worker hosts,
// before workers.Serve (or any other blocking entry point) is called in
// main — see the package doc comment.
//
// Calling Register more than once for the same className overwrites the
// previously registered Constructor.
func Register(className string, ctor Constructor) {
	constructorsMu.Lock()
	defer constructorsMu.Unlock()
	constructors[className] = ctor
}

// instanceOnce is a *sync.Once (rather than a sync.Once value) so that
// host_test.go can reset instance state between test cases by swapping the
// pointer instead of copying a sync.Once (which go vet's copylocks check
// rightly flags).
var (
	instanceOnce  = &sync.Once{}
	instanceErr   error
	instanceObj   Object
	instanceState *DurableObjectState
)

// State returns the current Durable Object instance's DurableObjectState.
// It is only meaningful once an instance has been constructed, i.e. from
// within Object.ServeHTTP or one of the optional handler interfaces'
// methods (or anything they call), all of which run only after that.
func State() *DurableObjectState {
	return instanceState
}

// instance returns this wasm instance's Object, constructing it (via the
// Constructor registered for the runtime context's "durableObject.className")
// exactly once — every subsequent trigger delivered to the same instance
// (fetch, alarm, webSocket*) reuses the same Object and DurableObjectState.
func instance() (Object, error) {
	instanceOnce.Do(func() {
		className, err := currentClassName()
		if err != nil {
			instanceErr = err
			return
		}
		constructorsMu.Lock()
		ctor, ok := constructors[className]
		constructorsMu.Unlock()
		if !ok {
			instanceErr = fmt.Errorf("durableobjects: no Constructor registered for class %q; call durableobjects.Register before workers.Serve", className)
			return
		}
		ctxVal, err := jsrt.RuntimeContextValue("ctx")
		if err != nil {
			instanceErr = fmt.Errorf("durableobjects: %w", err)
			return
		}
		envVal, err := jsrt.RuntimeContextValue("env")
		if err != nil {
			instanceErr = fmt.Errorf("durableobjects: %w", err)
			return
		}
		instanceState = DurableObjectStateFromJS(ctxVal)
		instanceObj, instanceErr = ctor(instanceState, envVal)
	})
	return instanceObj, instanceErr
}

// currentClassName reads durableObject.className off the runtime context —
// set by worker.mjs's GoDurableObject#bind for every trigger dispatched to
// a Durable Object instance (see cmd/workers-assets-gen/assets/common/worker.mjs).
func currentClassName() (string, error) {
	do, err := jsrt.RuntimeContextValue("durableObject")
	if err != nil {
		return "", fmt.Errorf("no \"durableObject\" runtime context value (was this triggered as a Durable Object? see cmd/workers-assets-gen's -durable-objects flag): %w", err)
	}
	name := do.Get("className")
	if jsrt.IsNil(name) {
		return "", errors.New("durableObject.className is not set")
	}
	return name.String(), nil
}

func init() {
	jsutil.RegisterAsyncHandler("handleDurableObjectFetch", 1, func(args []js.Value) (js.Value, error) {
		return handleFetch(args[0])
	})
	jsutil.RegisterAsyncHandler("handleDurableObjectAlarm", 1, func(args []js.Value) (js.Value, error) {
		var infoVal js.Value
		if len(args) > 0 {
			infoVal = args[0]
		}
		return js.Undefined(), handleAlarm(infoVal)
	})
	jsutil.RegisterAsyncHandler("handleDurableObjectWebSocketMessage", 2, func(args []js.Value) (js.Value, error) {
		return js.Undefined(), handleWebSocketMessage(args[0], args[1])
	})
	jsutil.RegisterAsyncHandler("handleDurableObjectWebSocketClose", 4, func(args []js.Value) (js.Value, error) {
		return js.Undefined(), handleWebSocketClose(args[0], args[1], args[2], args[3])
	})
	jsutil.RegisterAsyncHandler("handleDurableObjectWebSocketError", 2, func(args []js.Value) (js.Value, error) {
		return js.Undefined(), handleWebSocketError(args[0], args[1])
	})
}

// handleFetch dispatches the Durable Object's fetch() trigger to the
// instance's Object via jshttp.ServeRequest. onBodyClosed is nil: unlike
// handler_js.go's handleRequest (one wasm instance per request), a Durable
// Object's wasm instance stays alive for its whole lifetime, receiving
// further triggers, so there is no "done" signal to close here.
func handleFetch(reqObj js.Value) (js.Value, error) {
	obj, err := instance()
	if err != nil {
		return js.Value{}, err
	}
	return jshttp.ServeRequest(obj, reqObj, nil)
}

func handleAlarm(infoVal js.Value) error {
	obj, err := instance()
	if err != nil {
		return err
	}
	h, ok := obj.(AlarmHandler)
	if !ok {
		return fmt.Errorf("durableobjects: %T does not implement durableobjects.AlarmHandler", obj)
	}
	var info *AlarmInvocationInfo
	if !jsrt.IsNil(infoVal) {
		decoded, err := alarmInvocationInfoFromJS(infoVal)
		if err != nil {
			return err
		}
		info = &decoded
	}
	return h.Alarm(context.Background(), info)
}

func handleWebSocketMessage(ws, message js.Value) error {
	obj, err := instance()
	if err != nil {
		return err
	}
	h, ok := obj.(WebSocketMessageHandler)
	if !ok {
		return fmt.Errorf("durableobjects: %T does not implement durableobjects.WebSocketMessageHandler", obj)
	}
	return h.WebSocketMessage(context.Background(), ws, message)
}

func handleWebSocketClose(ws, codeVal, reasonVal, wasCleanVal js.Value) error {
	obj, err := instance()
	if err != nil {
		return err
	}
	h, ok := obj.(WebSocketCloseHandler)
	if !ok {
		return fmt.Errorf("durableobjects: %T does not implement durableobjects.WebSocketCloseHandler", obj)
	}
	return h.WebSocketClose(context.Background(), ws, codeVal.Int(), reasonVal.String(), wasCleanVal.Bool())
}

func handleWebSocketError(ws, errVal js.Value) error {
	obj, err := instance()
	if err != nil {
		return err
	}
	h, ok := obj.(WebSocketErrorHandler)
	if !ok {
		return fmt.Errorf("durableobjects: %T does not implement durableobjects.WebSocketErrorHandler", obj)
	}
	return h.WebSocketError(context.Background(), ws, errorFromJS(errVal))
}

// errorFromJS converts a thrown JS value (typically an Error) into a Go
// error, preferring its message property and falling back to toString() —
// mirroring the treatment jsutil.AwaitPromise gives a Promise rejection.
func errorFromJS(v js.Value) (err error) {
	if jsrt.IsNil(v) {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("durableobjects: %v", r)
		}
	}()
	if msg := v.Get("message"); msg.Type() == js.TypeString {
		return errors.New(msg.String())
	}
	return errors.New(v.Call("toString").String())
}
