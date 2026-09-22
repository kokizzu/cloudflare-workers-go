//go:build js && wasm

package rpc

import (
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeRemote builds a JS object with methods "add" (resolves a+b) and
// "echo" (resolves its single argument unchanged) -- good enough to
// exercise Stub.Call/CallJSON the way a real RpcStub's method call
// (always Promise-returning, per Workers RPC) would.
func fakeRemote() js.Value {
	v := jsrt.NewObject()
	v.Set("add", js.FuncOf(func(_ js.Value, args []js.Value) any {
		sum := args[0].Int() + args[1].Int()
		return jsutil.PromiseClass.Call("resolve", sum)
	}))
	v.Set("echo", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return jsutil.PromiseClass.Call("resolve", args[0])
	}))
	return v
}

// TestStub_Call_AwaitsResolvedValue verifies Call invokes the named method
// and awaits its (always-Promise) result.
func TestStub_Call_AwaitsResolvedValue(t *testing.T) {
	stub := StubFromJS(fakeRemote())
	res, err := stub.Call("add", 1, 2)
	if err != nil {
		t.Fatalf("Call() failed: %v", err)
	}
	if res.Int() != 3 {
		t.Errorf("Call() = %v, want 3", res.Int())
	}
}

// TestStub_Call_UnknownMethodErrors verifies calling a method the remote
// value doesn't have surfaces as a Go error (via jsrt.Call's panic
// recovery) instead of crashing.
func TestStub_Call_UnknownMethodErrors(t *testing.T) {
	stub := StubFromJS(fakeRemote())
	if _, err := stub.Call("missing"); err == nil {
		t.Fatal("Call() succeeded for an undefined method, want an error")
	}
}

// TestStub_CallJSON_DecodesResult verifies CallJSON JSON round-trips the
// resolved value into a typed Go value.
func TestStub_CallJSON_DecodesResult(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	stub := StubFromJS(fakeRemote())
	arg := js.ValueOf(map[string]any{"name": "go"})

	var out payload
	if err := stub.CallJSON("echo", &out, arg); err != nil {
		t.Fatalf("CallJSON() failed: %v", err)
	}
	if out.Name != "go" {
		t.Errorf("CallJSON() = %+v, want {Name:go}", out)
	}
}

// TestStub_CallJSON_NilResultLeavesOutUntouched verifies CallJSON is a
// no-op on out when the resolved value is undefined/null.
func TestStub_CallJSON_NilResultLeavesOutUntouched(t *testing.T) {
	v := jsrt.NewObject()
	v.Set("noop", js.FuncOf(func(_ js.Value, args []js.Value) any {
		return jsutil.PromiseClass.Call("resolve", js.Undefined())
	}))
	stub := StubFromJS(v)

	out := "unchanged"
	if err := stub.CallJSON("noop", &out); err != nil {
		t.Fatalf("CallJSON() failed: %v", err)
	}
	if out != "unchanged" {
		t.Errorf("CallJSON() modified out to %q, want it left untouched", out)
	}
}
