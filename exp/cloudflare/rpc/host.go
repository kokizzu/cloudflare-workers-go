//go:build js && wasm

package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// This file hosts a Go implementation of a WorkerEntrypoint's RPC methods
// and its own fetch() trigger, mirroring exp/cloudflare/durableobjects/
// host.go's and exp/cloudflare/workflows/host.go's approach: worker.mjs's
// GoWorkerEntrypoint#_bind sets up a "entrypoint: {className}" runtime
// context entry (cmd/workers-assets-gen/assets/common/worker.mjs), and each
// generated subclass method (one per name in workers-assets-gen's
// -entrypoints=Name:method1,method2 flag) forwards to handleRPC, plus a
// fetch method every generated subclass has that forwards to
// handleEntrypointFetch.
//
// Unlike a Durable Object, a WorkerEntrypoint's RPC methods don't need
// cross-call Go state kept alive by this package itself (a Go program can
// still keep its own state across calls the normal way, e.g. package-level
// variables) — every call here just dispatches to the Method registered
// for (className, methodName) or the http.Handler registered for
// className's fetch().

// Method is the Go implementation of one RPC method a Go-hosted
// WorkerEntrypoint exposes. args holds the JS values the caller passed, in
// call order (each is structured-clonable, per Workers RPC's argument
// rules); the returned js.Value becomes the RPC call's resolved result
// (also structured-clonable). Use MethodJSON to work with typed Go values
// instead of raw js.Value args/results.
type Method func(ctx context.Context, args []js.Value) (js.Value, error)

// MethodJSON adapts a typed Go function into a Method. Since RPC arguments
// arrive positionally, with no fixed arity known to this package (a
// generated method's own JS signature is "...args", per
// tmp/06-codegen-spec.md 8.1), fn receives each argument individually
// JSON-encoded (via the JS side's JSON.stringify) as a json.RawMessage —
// decode as many of them, into whatever shape, as the method expects
// (e.g. `var a, b int; json.Unmarshal(args[0], &a); json.Unmarshal(args[1],
// &b)` for a two-int-argument method). fn's returned Out is JSON-encoded
// back into the structured-clonable js.Value the RPC call resolves with,
// the same round trip exp/cloudflare/workflows.DoJSON uses for a step's
// result.
func MethodJSON[Out any](fn func(ctx context.Context, args []json.RawMessage) (Out, error)) Method {
	return func(ctx context.Context, args []js.Value) (js.Value, error) {
		raw := make([]json.RawMessage, len(args))
		for i, a := range args {
			b, err := argToJSON(a)
			if err != nil {
				return js.Value{}, err
			}
			raw[i] = b
		}
		out, err := fn(ctx, raw)
		if err != nil {
			return js.Value{}, err
		}
		return valueToJS(out)
	}
}

// argToJSON JSON-encodes a JS argument value (via JSON.stringify) into a
// json.RawMessage. An undefined/null v encodes as JSON null.
func argToJSON(v js.Value) (json.RawMessage, error) {
	if jsrt.IsNil(v) {
		return json.RawMessage("null"), nil
	}
	s, err := jsrt.Call(js.Global().Get("JSON"), "stringify", v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(s.String()), nil
}

// valueToJS JSON-encodes v (via encoding/json.Marshal) and parses the
// result back into a structured-clonable JS value (JSON.parse) — the same
// round trip exp/cloudflare/workflows.ResultJSON uses for a Runner's
// result.
func valueToJS(v any) (js.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return js.Value{}, err
	}
	return jsrt.Call(js.Global().Get("JSON"), "parse", string(b))
}

var (
	methodsMu     sync.Mutex
	methodsByName = map[string]map[string]Method{}

	fetchMu     sync.Mutex
	fetchByName = map[string]http.Handler{}
)

// Register associates className (matching both a "Name" entry in the
// -entrypoints flag passed to workers-assets-gen and the className the
// generated subclass reports via its goClassName) with methods: the set of
// RPC methods that class exposes. A method name in methods must also be
// listed for that class in -entrypoints=Name:method1,method2,... — the JS
// side only forwards calls for method names it was generated with — but a
// name generated for the class with no Register-ed Method rejects that
// call at request time instead of failing to build.
//
// Register must be called for every WorkerEntrypoint class this Worker
// hosts RPC methods for, before workers.Serve (or any other blocking entry
// point) is called in main. Calling Register more than once for the same
// className replaces its whole method set (it does not merge with a
// previous call).
func Register(className string, methods map[string]Method) {
	methodsMu.Lock()
	defer methodsMu.Unlock()
	methodsByName[className] = methods
}

// RegisterFetch associates className with h: the http.Handler that serves
// the WorkerEntrypoint's own fetch() trigger (distinct from this Worker's
// regular default-export fetch handler passed to workers.Serve — this one
// runs when something calls .fetch() on a stub bound to this named
// entrypoint, e.g. a Service binding with no `entrypoint` given an upgrade
// request, or the Cloudflare dashboard's preview for that binding).
//
// RegisterFetch must be called before workers.Serve for every class whose
// fetch() trigger this Worker should handle; a class generated via
// -entrypoints without a corresponding RegisterFetch call rejects its
// fetch() calls at request time.
func RegisterFetch(className string, h http.Handler) {
	fetchMu.Lock()
	defer fetchMu.Unlock()
	fetchByName[className] = h
}

func init() {
	jsutil.RegisterAsyncHandler("handleRPC", 2, func(args []js.Value) (js.Value, error) {
		if len(args) < 2 {
			return js.Value{}, fmt.Errorf("rpc: handleRPC requires 2 arguments (name, args), got %d", len(args))
		}
		className, err := currentClassName()
		if err != nil {
			return js.Value{}, err
		}
		methodName := args[0].String()
		methodsMu.Lock()
		methods, ok := methodsByName[className]
		methodsMu.Unlock()
		if !ok {
			return js.Value{}, fmt.Errorf("rpc: no methods registered for class %q; call rpc.Register before workers.Serve", className)
		}
		method, ok := methods[methodName]
		if !ok {
			return js.Value{}, fmt.Errorf("rpc: class %q has no RPC method %q registered", className, methodName)
		}
		return method(context.Background(), jsArrayToSlice(args[1]))
	})
	jsutil.RegisterAsyncHandler("handleEntrypointFetch", 1, func(args []js.Value) (js.Value, error) {
		if len(args) < 1 {
			return js.Value{}, fmt.Errorf("rpc: handleEntrypointFetch requires 1 argument (req), got %d", len(args))
		}
		className, err := currentClassName()
		if err != nil {
			return js.Value{}, err
		}
		fetchMu.Lock()
		h, ok := fetchByName[className]
		fetchMu.Unlock()
		if !ok {
			return js.Value{}, fmt.Errorf("rpc: no fetch handler registered for class %q; call rpc.RegisterFetch before workers.Serve", className)
		}
		return jshttp.ServeRequest(h, args[0], nil)
	})
}

// jsArrayToSlice reads a JS array value (handleRPC's "args" argument,
// built from the generated subclass method's "...args" rest parameter)
// into a []js.Value.
func jsArrayToSlice(v js.Value) []js.Value {
	if jsrt.IsNil(v) {
		return nil
	}
	n := v.Get("length").Int()
	out := make([]js.Value, n)
	for i := 0; i < n; i++ {
		out[i] = v.Index(i)
	}
	return out
}

// currentClassName reads entrypoint.className off the runtime context —
// set by worker.mjs's GoWorkerEntrypoint#_bind for every RPC call or
// fetch() trigger dispatched to a WorkerEntrypoint instance (see
// cmd/workers-assets-gen/assets/common/worker.mjs).
func currentClassName() (string, error) {
	ep, err := jsrt.RuntimeContextValue("entrypoint")
	if err != nil {
		return "", fmt.Errorf("rpc: no \"entrypoint\" runtime context value (was this triggered as a WorkerEntrypoint? see cmd/workers-assets-gen's -entrypoints flag): %w", err)
	}
	name := ep.Get("className")
	if jsrt.IsNil(name) {
		return "", errors.New("rpc: entrypoint.className is not set")
	}
	return name.String(), nil
}
