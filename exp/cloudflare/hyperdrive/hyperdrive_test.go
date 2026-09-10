//go:build js && wasm

package hyperdrive

import (
	"context"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeSocketValue builds a JS object shaped enough like the Socket
// cloudflare/sockets.Connect's newSocket expects (writable.getWriter(),
// readable, startTls, close) that constructing a *sockets.Socket around it
// doesn't panic -- newSocket calls writable.getWriter() eagerly, but this
// test never actually reads/writes/closes the connection.
func fakeSocketValue() js.Value {
	writer := jsutil.NewObject()
	writable := jsutil.NewObject()
	writable.Set("getWriter", js.FuncOf(func(this js.Value, args []js.Value) any {
		return writer
	}))
	sock := jsutil.NewObject()
	sock.Set("writable", writable)
	sock.Set("readable", jsutil.NewObject())
	sock.Set("startTls", js.FuncOf(func(this js.Value, args []js.Value) any {
		return fakeSocketValue()
	}))
	sock.Set("close", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.Undefined()
	}))
	return sock
}

// withFakeTryCatch installs a globalThis.tryCatch good enough for
// cloudflare/sockets.Connect's jsutil.TryCatch call: it just invokes fn and
// wraps the result as { result }, skipping the real shim's (worker.mjs's)
// catch branch -- this test's fake connect() never throws, so that's never
// exercised. The real tryCatch global is only installed by worker.mjs at
// runtime, not by the `go test` wasm harness, so tests exercising any code
// path through jsutil.TryCatch need to fake it themselves.
func withFakeTryCatch(t *testing.T) {
	t.Helper()
	fn := js.FuncOf(func(this js.Value, args []js.Value) any {
		out := jsutil.NewObject()
		out.Set("result", args[0].Invoke())
		return out
	})
	prev := js.Global().Get("tryCatch")
	js.Global().Set("tryCatch", fn)
	t.Cleanup(func() {
		fn.Release()
		if prev.IsUndefined() {
			js.Global().Delete("tryCatch")
		} else {
			js.Global().Set("tryCatch", prev)
		}
	})
}

// TestConnect_DialsHostPort verifies that Hyperdrive.Connect calls the
// runtime context's connect() with "host:port" built from Host()/Port(), as
// tmp/06-codegen-spec.md 5.2 item 1 requires.
func TestConnect_DialsHostPort(t *testing.T) {
	withFakeTryCatch(t)

	prevCtx := jsutil.RuntimeContext
	t.Cleanup(func() { jsutil.RuntimeContext = prevCtx })

	var gotAddr string
	connectFn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			gotAddr = args[0].String()
		}
		return fakeSocketValue()
	})
	t.Cleanup(connectFn.Release)

	rc := jsutil.NewObject()
	rc.Set("connect", connectFn)
	jsutil.RuntimeContext = rc

	h := HyperdriveFromJS(js.ValueOf(map[string]any{
		"host": "db.example.internal",
		"port": 3306.0,
	}))

	conn, err := h.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer conn.Close()

	if want := "db.example.internal:3306"; gotAddr != want {
		t.Errorf("connect() called with addr = %q, want %q", gotAddr, want)
	}
}
