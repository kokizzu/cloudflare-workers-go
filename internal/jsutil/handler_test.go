package jsutil

import (
	"syscall/js"
	"testing"
)

// TestAsyncFunc_RecoversPanic verifies that a panic inside AsyncFunc's
// handler rejects the Promise instead of crashing the wasm program (which
// would leave the Promise pending forever and take the whole instance down
// with it).
func TestAsyncFunc_RecoversPanic(t *testing.T) {
	fn := AsyncFunc(func(args []js.Value) (js.Value, error) {
		panic("boom")
	})
	defer fn.Release()

	if _, err := AwaitPromise(fn.Invoke()); err == nil {
		t.Fatal("AsyncFunc did not reject the Promise after handler panicked")
	}
}

// TestAsyncFunc_ResolvesNormally is a control case: a handler that returns
// normally still resolves the Promise with its result, unaffected by the
// panic recovery added around it.
func TestAsyncFunc_ResolvesNormally(t *testing.T) {
	fn := AsyncFunc(func(args []js.Value) (js.Value, error) {
		return js.ValueOf("ok"), nil
	})
	defer fn.Release()

	result, err := AwaitPromise(fn.Invoke())
	if err != nil {
		t.Fatalf("AwaitPromise returned error: %v", err)
	}
	if got, want := result.String(), "ok"; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

// TestRegisterAsyncHandler_RecoversPanic is RegisterAsyncHandler's analogue
// of TestAsyncFunc_RecoversPanic: a panicking handler registered on
// jsutil.Binding must reject rather than crash the program.
func TestRegisterAsyncHandler_RecoversPanic(t *testing.T) {
	RegisterAsyncHandler("testHandlerPanic", 0, func(args []js.Value) (js.Value, error) {
		panic("boom")
	})

	fn := Binding.Get("testHandlerPanic")
	if _, err := AwaitPromise(fn.Invoke()); err == nil {
		t.Fatal("RegisterAsyncHandler did not reject the Promise after handler panicked")
	}
}
