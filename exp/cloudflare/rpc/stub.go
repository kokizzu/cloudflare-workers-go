//go:build js && wasm

// Package rpc provides a caller-side, dynamically-typed client for
// Cloudflare Workers RPC (calling a method on a WorkerEntrypoint — a named
// entrypoint reached over a Service binding, or a Durable Object's own
// methods beyond fetch()), plus the implementation-side registration and
// dispatch a Go-hosted WorkerEntrypoint uses to expose such methods. See
// tmp/06-codegen-spec.md 8.
//
// Workers RPC's typed stubs (TypeScript's Rpc.Provider<T>) can't be
// generated for statically-typed Go — there's no way to derive a Go method
// set from an arbitrary remote class — so the caller side ([Stub]) is
// dynamic instead: [Stub.Call] takes a method name and untyped arguments
// and returns an untyped result (or [Stub.CallJSON] to decode that result
// into a typed Go value via JSON). This file is entirely hand-written (no
// generated L1 involved).
package rpc

import (
	"encoding/json"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// Stub is a dynamically-typed RPC client wrapping a JS value that behaves
// like an RpcStub: a Service binding pointing at a named WorkerEntrypoint
// (wrangler.toml's [[services]] entrypoint), or a Durable Object stub
// (cloudflare.DurableObjectStub.RPC()) calling one of its own methods
// beyond fetch().
type Stub struct{ v js.Value }

// StubFromJS wraps an existing JS value (e.g. a Service binding fetched via
// cloudflare.GetBinding, a DurableObjectStub's underlying JS value, or a
// stub/function previously returned by Call) as a Stub.
func StubFromJS(v js.Value) *Stub {
	return &Stub{v: v}
}

// NewStub resolves bindingName from the Worker's env bindings — typically a
// Service binding configured with an `entrypoint` in wrangler.toml's
// [[services]], pointing at a WorkerEntrypoint class (hosted by this Worker
// or another one) — and wraps it as a Stub.
func NewStub(bindingName string) (*Stub, error) {
	v, err := jsrt.Binding(bindingName)
	if err != nil {
		return nil, err
	}
	return StubFromJS(v), nil
}

// JS returns the Stub's underlying JS value.
func (s *Stub) JS() js.Value {
	return s.v
}

// Call invokes method on the remote entrypoint with args and awaits its
// result. Each arg is converted the way [syscall/js.ValueOf] converts a Go
// value to JS: primitive types, [syscall/js.Value], types implementing
// syscall/js.Wrapper, and (recursively) map[string]any/[]any are all
// supported; anything else panics, per js.ValueOf's own documented
// behavior. An RPC method's return value is always a Promise on the JS
// side (https://developers.cloudflare.com/workers/runtime-apis/rpc/), so
// Call always awaits it.
//
// If the resolved value is itself an RPC stub or a function (e.g. a method
// that returns another Rpc.Target), it's returned as-is — wrap it in
// another Stub via StubFromJS to keep calling into it.
func (s *Stub) Call(method string, args ...any) (js.Value, error) {
	p, err := jsrt.Call(s.v, method, args...)
	if err != nil {
		return js.Value{}, err
	}
	return jsrt.Await(p)
}

// CallJSON is Call for a typed Go result: the resolved value is
// JSON-encoded on the JS side (JSON.stringify) and decoded into out (a
// pointer, as for [encoding/json.Unmarshal]), the same round trip
// exp/cloudflare/workflows.Event.PayloadJSON uses. If the resolved value is
// undefined or null, out is left untouched.
func (s *Stub) CallJSON(method string, out any, args ...any) error {
	res, err := s.Call(method, args...)
	if err != nil {
		return err
	}
	if jsrt.IsNil(res) {
		return nil
	}
	str, err := jsrt.Call(js.Global().Get("JSON"), "stringify", res)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(str.String()), out)
}
