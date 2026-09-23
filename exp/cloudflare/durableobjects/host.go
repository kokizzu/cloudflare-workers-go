//go:build js && wasm

package durableobjects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/exp/cloudflare/websocket"
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
// connection accepted via DurableObjectState.AcceptHibernatingConn (the
// hibernatable WebSocket API). mt/data decode the JS side's
// `string | ArrayBuffer` message payload the same way
// exp/cloudflare/websocket.Conn.ReadMessage does (TextMessage for a JS
// string, BinaryMessage otherwise).
type WebSocketMessageHandler interface {
	WebSocketMessage(ctx context.Context, conn *websocket.HibernatingConn, mt websocket.MessageType, data []byte) error
}

// WebSocketCloseHandler is implemented by an Object that wants to receive
// the Durable Object's webSocketClose() trigger.
type WebSocketCloseHandler interface {
	WebSocketClose(ctx context.Context, conn *websocket.HibernatingConn, code int, reason string, wasClean bool) error
}

// WebSocketErrorHandler is implemented by an Object that wants to receive
// the Durable Object's webSocketError() trigger.
type WebSocketErrorHandler interface {
	WebSocketError(ctx context.Context, conn *websocket.HibernatingConn, err error) error
}

// Constructor builds an Object for one Durable Object instance, given its
// DurableObjectState (ctx) and environment bindings (env). It runs the
// first time any trigger reaches this wasm instance; if it returns a
// non-nil error, it is retried from scratch on the next trigger (a
// Constructor failure is often transient — e.g. a binding or storage read
// racing instance startup — and a Durable Object's wasm instance otherwise
// stays alive indefinitely, so caching the failure would turn one bad
// attempt into a permanent outage for that object id). Once it returns
// successfully, the result is memoized for the rest of the wasm instance's
// lifetime (i.e. every subsequent trigger delivered to the same Durable
// Object instance reuses it) and Constructor is not called again — see
// instance()'s doc comment.
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

// instanceMu guards instanceObj/instanceState/instanceReady below.
//
// Construction is memoized only on success (instanceReady), not with a
// sync.Once: a Constructor error (e.g. a binding that isn't ready yet, or
// some other transient failure) is deliberately not cached, so the next
// trigger delivered to this wasm instance retries the Constructor instead of
// failing forever. A Durable Object instance's wasm instance can stay alive
// for a long time (see the package doc comment), so permanently wedging it
// after one failed construction would otherwise require the runtime to evict
// and recreate the whole instance to recover.
var (
	instanceMu    sync.Mutex
	instanceReady bool
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
// on the first successful call — every subsequent trigger delivered to the
// same instance (fetch, alarm, webSocket*) reuses that same Object and
// DurableObjectState. A Constructor error is not cached: it is returned to
// this call's caller, and the next trigger tries construction again (see the
// instanceMu doc comment).
func instance() (Object, error) {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instanceReady {
		return instanceObj, nil
	}
	className, err := currentClassName()
	if err != nil {
		return nil, err
	}
	constructorsMu.Lock()
	ctor, ok := constructors[className]
	constructorsMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("durableobjects: no Constructor registered for class %q; call durableobjects.Register before workers.Serve", className)
	}
	ctxVal, err := jsrt.RuntimeContextValue("ctx")
	if err != nil {
		return nil, fmt.Errorf("durableobjects: %w", err)
	}
	envVal, err := jsrt.RuntimeContextValue("env")
	if err != nil {
		return nil, fmt.Errorf("durableobjects: %w", err)
	}
	state := DurableObjectStateFromJS(ctxVal)
	obj, err := ctor(state, envVal)
	if err != nil {
		return nil, err
	}
	instanceState = state
	instanceObj = obj
	instanceReady = true
	return instanceObj, nil
}

// currentClassName reads durableObject.className off the runtime context —
// set by worker.mjs's GoDurableObject#bind for every trigger dispatched to
// a Durable Object instance (see cmd/workers-assets-gen/assets/runtimes/cloudflare/worker.mjs).
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
	mt, data := decodeHibernatingMessage(message)
	return h.WebSocketMessage(context.Background(), websocket.HibernatingConnFromJS(ws), mt, data)
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
	return h.WebSocketClose(context.Background(), websocket.HibernatingConnFromJS(ws), codeVal.Int(), reasonVal.String(), wasCleanVal.Bool())
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
	return h.WebSocketError(context.Background(), websocket.HibernatingConnFromJS(ws), errorFromJS(errVal))
}

// decodeHibernatingMessage decodes a webSocketMessage() trigger's message
// argument — the JS side's `string | ArrayBuffer` union — the same way
// exp/cloudflare/websocket.Conn's "message" event listener does: a JS
// string becomes TextMessage, anything else (an ArrayBuffer) becomes
// BinaryMessage.
func decodeHibernatingMessage(v js.Value) (websocket.MessageType, []byte) {
	if v.Type() == js.TypeString {
		return websocket.TextMessage, []byte(v.String())
	}
	// js.CopyBytesToGo (via jsrt.BytesFromJS) requires a Uint8Array view,
	// not a bare ArrayBuffer.
	view := js.Global().Get("Uint8Array").New(v)
	return websocket.BinaryMessage, jsrt.BytesFromJS(view)
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
