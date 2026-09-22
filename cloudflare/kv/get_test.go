//go:build js && wasm

package kv

import (
	"syscall/js"
	"testing"

	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"
)

// TestNamespace_GetStrings exercises the L2 GetStrings wrapper (added by
// tmp/06-codegen-spec.md 6.2 item 3) around the generated L1
// KVNamespace.GetTextMultiple: a fake KVNamespace whose "get" resolves with
// a JS Map holding a string for one key and null for another (workers-types'
// Map<string, string | null> shape for a missing key), verifying the
// missing key is left out of the result map rather than appearing as "".
func TestNamespace_GetStrings(t *testing.T) {
	var gotKeys []string
	var gotType string
	fake := js.ValueOf(map[string]any{})
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		keysArr := args[0]
		for i := 0; i < keysArr.Length(); i++ {
			gotKeys = append(gotKeys, keysArr.Index(i).String())
		}
		gotType = args[1].Get("type").String()

		m := js.Global().Get("Map").New()
		m.Call("set", "present", "value-1")
		m.Call("set", "missing", js.Null())
		return js.Global().Get("Promise").Call("resolve", m)
	}))

	ns := &Namespace{instance: kvjs.KVNamespaceFromJS(fake)}
	got, err := ns.GetStrings([]string{"present", "missing"}, nil)
	if err != nil {
		t.Fatalf("GetStrings() failed: %v", err)
	}
	if len(gotKeys) != 2 || gotKeys[0] != "present" || gotKeys[1] != "missing" {
		t.Errorf("keys sent to JS = %v, want [present missing]", gotKeys)
	}
	if gotType != "text" {
		t.Errorf("type sent to JS = %q, want %q", gotType, "text")
	}
	if want := map[string]string{"present": "value-1"}; len(got) != len(want) || got["present"] != want["present"] {
		t.Errorf("GetStrings() = %v, want %v", got, want)
	}
	if _, ok := got["missing"]; ok {
		t.Errorf("GetStrings() included %q for a missing key, want it omitted", "missing")
	}
}
