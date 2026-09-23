//go:build js && wasm

package secretsstore

import (
	"syscall/js"
	"testing"
)

// TestSecretsStoreSecret_Get exercises the full generated round trip for a
// Promise-returning method on a handle type: a fake JS SecretsStoreSecret
// object whose get() returns a resolved Promise, wrapped with
// SecretsStoreSecretFromJS, called through Get, and decoded back into a
// string.
func TestSecretsStoreSecret_Get(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.Global().Get("Promise").Call("resolve", js.ValueOf("s3cr3t"))
	}))

	secret := SecretsStoreSecretFromJS(fake)
	got, err := secret.Get()
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("Get() = %q, want %q", got, "s3cr3t")
	}
}

// TestSecretsStoreSecret_Get_Rejects verifies a rejected Promise surfaces as
// a Go error rather than panicking.
func TestSecretsStoreSecret_Get_Rejects(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New("boom"))
	}))

	secret := SecretsStoreSecretFromJS(fake)
	if _, err := secret.Get(); err == nil {
		t.Fatalf("Get() succeeded, want an error")
	}
}
