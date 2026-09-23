//go:build js && wasm

package jsutil

import (
	"syscall/js"
	"testing"
)

func TestTryCatch(t *testing.T) {
	t.Run("returns_value", func(t *testing.T) {
		got, err := TryCatch(func() js.Value {
			return js.ValueOf("ok")
		})
		if err != nil {
			t.Fatalf("TryCatch() error = %v, want nil", err)
		}
		if got.String() != "ok" {
			t.Errorf("TryCatch() = %v, want %q", got, "ok")
		}
	})

	t.Run("returns_undefined", func(t *testing.T) {
		got, err := TryCatch(func() js.Value {
			return js.Undefined()
		})
		if err != nil {
			t.Fatalf("TryCatch() error = %v, want nil", err)
		}
		if !got.IsUndefined() {
			t.Errorf("TryCatch() = %v, want undefined", got)
		}
	})

	t.Run("throws", func(t *testing.T) {
		// A JS call that throws inside fn surfaces as a Go panic from
		// Value.Call; TryCatch's wrapper recovers it into an error.
		if _, err := TryCatch(func() js.Value {
			js.Global().Call("eval", "throw new Error('js throw')")
			return js.Undefined()
		}); err == nil {
			t.Fatal("TryCatch() error = nil, want non-nil for a JS throw")
		}
	})

	t.Run("panics", func(t *testing.T) {
		// fn panics on the Go side - e.g. Value.Invoke failing, which is
		// how cloudflare/sockets.Connect surfaces a rejected connect()
		// call. The panic is recovered inside the js.FuncOf callback;
		// letting it escape in this reentrant position hangs the process.
		got, err := TryCatch(func() js.Value {
			panic("go panic")
		})
		if err == nil {
			t.Fatalf("TryCatch() = %v, nil; want a non-nil error for a Go panic", got)
		}
	})
}
