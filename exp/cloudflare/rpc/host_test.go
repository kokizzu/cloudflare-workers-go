//go:build js && wasm

package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// resetState clears the package-level Register/RegisterFetch state around
// a test, so tests don't leak into each other -- mirroring
// durableobjects/host_test.go's resetInstance and workflows/host_test.go's
// resetRunners.
func resetState(t *testing.T) {
	t.Helper()
	methodsMu.Lock()
	prevMethods := methodsByName
	methodsByName = map[string]map[string]Method{}
	methodsMu.Unlock()

	fetchMu.Lock()
	prevFetch := fetchByName
	fetchByName = map[string]http.Handler{}
	fetchMu.Unlock()

	t.Cleanup(func() {
		methodsMu.Lock()
		methodsByName = prevMethods
		methodsMu.Unlock()
		fetchMu.Lock()
		fetchByName = prevFetch
		fetchMu.Unlock()
	})
}

// withRuntimeContext installs a fake jsutil.RuntimeContext of the shape
// worker.mjs's GoWorkerEntrypoint#_bind builds for an RPC call or fetch()
// trigger (entrypoint: {className}), restoring the previous one via
// t.Cleanup -- mirroring durableobjects/host_test.go's and
// workflows/host_test.go's same-named helper.
func withRuntimeContext(t *testing.T, className string) {
	t.Helper()
	prev := jsutil.RuntimeContext
	rc := jsrt.NewObject()
	ep := jsrt.NewObject()
	ep.Set("className", className)
	rc.Set("entrypoint", ep)
	jsutil.RuntimeContext = rc
	t.Cleanup(func() { jsutil.RuntimeContext = prev })
}

// TestRegister_HandleRPC_Dispatches verifies that handleRPC (registered on
// jsutil.Binding by init()) looks up entrypoint.className in the runtime
// context, dispatches to the Method registered for that (className,
// methodName) pair, and resolves with its result.
func TestRegister_HandleRPC_Dispatches(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "MyService")

	var gotArgs []js.Value
	Register("MyService", map[string]Method{
		"add": func(ctx context.Context, args []js.Value) (js.Value, error) {
			gotArgs = args
			return js.ValueOf(args[0].Int() + args[1].Int()), nil
		},
	})

	handle := jsutil.Binding.Get("handleRPC")
	if handle.IsUndefined() {
		t.Fatal("init() did not register \"handleRPC\" on jsutil.Binding")
	}

	argsArray := js.ValueOf([]any{1, 2})
	result, err := jsrt.Await(handle.Invoke("add", argsArray))
	if err != nil {
		t.Fatalf("handleRPC rejected: %v", err)
	}
	if result.Int() != 3 {
		t.Errorf("result = %v, want 3", result.Int())
	}
	if len(gotArgs) != 2 || gotArgs[0].Int() != 1 || gotArgs[1].Int() != 2 {
		t.Errorf("Method got args %v, want [1 2]", gotArgs)
	}
}

// TestRegister_HandleRPC_UnregisteredClassName verifies a className with no
// Register-ed methods rejects the Promise.
func TestRegister_HandleRPC_UnregisteredClassName(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "NotRegistered")

	handle := jsutil.Binding.Get("handleRPC")
	if _, err := jsrt.Await(handle.Invoke("add", js.ValueOf([]any{}))); err == nil {
		t.Fatal("handleRPC resolved for an unregistered class name, want a rejection")
	}
}

// TestRegister_HandleRPC_UnregisteredMethod verifies a method name not
// present in the registered map rejects the Promise.
func TestRegister_HandleRPC_UnregisteredMethod(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "MyService")
	Register("MyService", map[string]Method{})

	handle := jsutil.Binding.Get("handleRPC")
	if _, err := jsrt.Await(handle.Invoke("missing", js.ValueOf([]any{}))); err == nil {
		t.Fatal("handleRPC resolved for an unregistered method, want a rejection")
	}
}

// TestRegister_HandleRPC_PropagatesMethodError verifies a Method error
// rejects the Promise.
func TestRegister_HandleRPC_PropagatesMethodError(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "MyService")
	Register("MyService", map[string]Method{
		"fail": func(ctx context.Context, args []js.Value) (js.Value, error) {
			return js.Value{}, errors.New("boom")
		},
	})

	handle := jsutil.Binding.Get("handleRPC")
	if _, err := jsrt.Await(handle.Invoke("fail", js.ValueOf([]any{}))); err == nil {
		t.Fatal("handleRPC resolved despite a Method error, want a rejection")
	}
}

// TestMethodJSON_RoundTripsTypedValue verifies MethodJSON JSON-decodes
// each positional arg and JSON-encodes the typed result back into a
// structured-clonable js.Value.
func TestMethodJSON_RoundTripsTypedValue(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "MyService")

	greet := MethodJSON(func(ctx context.Context, args []json.RawMessage) (string, error) {
		var name string
		if err := json.Unmarshal(args[0], &name); err != nil {
			return "", err
		}
		return fmt.Sprintf("hello, %s", name), nil
	})
	Register("MyService", map[string]Method{"greet": greet})

	handle := jsutil.Binding.Get("handleRPC")
	result, err := jsrt.Await(handle.Invoke("greet", js.ValueOf([]any{"go"})))
	if err != nil {
		t.Fatalf("handleRPC rejected: %v", err)
	}
	if result.String() != "hello, go" {
		t.Errorf("result = %q, want \"hello, go\"", result.String())
	}
}

// fakeRequest builds a real JS Request object (Node's global Request
// class), mirroring durableobjects/host_test.go's helper of the same name.
func fakeRequest(path string) js.Value {
	return jsutil.RequestClass.New("http://myservice.test"+path, js.ValueOf(map[string]any{
		"method": "GET",
	}))
}

func bodyText(t *testing.T, resp js.Value) string {
	t.Helper()
	v, err := jsrt.Await(resp.Call("text"))
	if err != nil {
		t.Fatalf("resp.text() failed: %v", err)
	}
	return v.String()
}

// TestRegisterFetch_HandleEntrypointFetch_Dispatches verifies that
// handleEntrypointFetch dispatches to the http.Handler registered for the
// runtime context's className via jshttp.ServeRequest.
func TestRegisterFetch_HandleEntrypointFetch_Dispatches(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "MyService")

	RegisterFetch("MyService", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "path=%s", r.URL.Path)
	}))

	handle := jsutil.Binding.Get("handleEntrypointFetch")
	if handle.IsUndefined() {
		t.Fatal("init() did not register \"handleEntrypointFetch\" on jsutil.Binding")
	}

	resp, err := jsrt.Await(handle.Invoke(fakeRequest("/hello")))
	if err != nil {
		t.Fatalf("handleEntrypointFetch rejected: %v", err)
	}
	if got := bodyText(t, resp); got != "path=/hello" {
		t.Errorf("response body = %q, want \"path=/hello\"", got)
	}
}

// TestRegisterFetch_HandleEntrypointFetch_UnregisteredClassName verifies a
// className with no RegisterFetch-ed handler rejects the Promise.
func TestRegisterFetch_HandleEntrypointFetch_UnregisteredClassName(t *testing.T) {
	resetState(t)
	withRuntimeContext(t, "NotRegistered")

	handle := jsutil.Binding.Get("handleEntrypointFetch")
	if _, err := jsrt.Await(handle.Invoke(fakeRequest("/"))); err == nil {
		t.Fatal("handleEntrypointFetch resolved for an unregistered class name, want a rejection")
	}
}
