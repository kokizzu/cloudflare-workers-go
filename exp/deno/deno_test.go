//go:build js && wasm

package deno

import (
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

func TestJsToAny_Primitives(t *testing.T) {
	if got := jsToAny(js.Undefined()); got != nil {
		t.Errorf("jsToAny(undefined) = %v, want nil", got)
	}
	if got := jsToAny(js.Null()); got != nil {
		t.Errorf("jsToAny(null) = %v, want nil", got)
	}
	if got := jsToAny(js.ValueOf(true)); got != true {
		t.Errorf("jsToAny(true) = %v, want true", got)
	}
	if got := jsToAny(js.ValueOf(1.5)); got != 1.5 {
		t.Errorf("jsToAny(1.5) = %v, want 1.5", got)
	}
	if got := jsToAny(js.ValueOf("s")); got != "s" {
		t.Errorf("jsToAny(\"s\") = %v, want \"s\"", got)
	}
}

func TestJsToAny_TypedValues(t *testing.T) {
	b := jsutil.Uint8ArrayClass.New(3)
	b.SetIndex(0, 1)
	b.SetIndex(1, 2)
	b.SetIndex(2, 3)
	if got := jsToAny(b); !equalBytes(got.([]byte), []byte{1, 2, 3}) {
		t.Errorf("jsToAny(Uint8Array) = %v, want [1 2 3]", got)
	}

	d := jsutil.TimeToDate(time.UnixMilli(1700000000000).UTC())
	if got := jsToAny(d); !got.(time.Time).Equal(time.UnixMilli(1700000000000).UTC()) {
		t.Errorf("jsToAny(Date) = %v, want 2023-11-14T22:13:20Z", got)
	}
}

func TestJsToAny_PlainObject(t *testing.T) {
	o := jsutil.NewObject()
	o.Set("s", "v")
	o.Set("n", 2)
	inner := jsutil.NewObject()
	inner.Set("b", true)
	o.Set("inner", inner)
	arr := jsutil.NewArray(2)
	arr.SetIndex(0, "a")
	arr.SetIndex(1, "b")
	o.Set("arr", arr)

	got := jsToAny(o)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("jsToAny(plain object) = %T, want map[string]any", got)
	}
	if m["s"] != "v" || m["n"] != 2.0 {
		t.Errorf("jsToAny(plain object) = %v, want {s: v, n: 2, ...}", m)
	}
	if im, ok := m["inner"].(map[string]any); !ok || im["b"] != true {
		t.Errorf("nested object = %v, want {b: true}", m["inner"])
	}
	if ia, ok := m["arr"].([]any); !ok || len(ia) != 2 || ia[0] != "a" {
		t.Errorf("nested array = %v, want [a b]", m["arr"])
	}
}

// A BigInt has no wasm ABI type flag: js.Value.Type() panics on it ("bad
// type flag"). It must pass through as a js.Value, both at top level and
// nested inside a plain object.
func TestJsToAny_BigInt(t *testing.T) {
	got := jsToAny(BigInt(42))
	if _, ok := got.(js.Value); !ok {
		t.Errorf("jsToAny(BigInt) = %T, want js.Value passthrough", got)
	}

	o := jsutil.NewObject()
	o.Set("value", BigInt(42))
	got = jsToAny(o)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("jsToAny({value: 42n}) = %T, want map[string]any", got)
	}
	if _, ok := m["value"].(js.Value); !ok {
		t.Errorf("jsToAny({value: 42n})[\"value\"] = %T, want js.Value passthrough", m["value"])
	}
}

// A class instance (Map here; KvU64 follows the same path) must not be
// flattened into a map — it is passed through as js.Value.
func TestJsToAny_ClassInstance(t *testing.T) {
	mp := js.Global().Get("Map").New()
	got := jsToAny(mp)
	if _, ok := got.(js.Value); !ok {
		t.Errorf("jsToAny(Map instance) = %T, want js.Value passthrough", got)
	}
}

// An object tagged like a Deno.KvU64 converts to *KvU64. (The real class
// requires Deno; Symbol.toStringTag drives the same branch.)
func TestJsToAny_KvU64(t *testing.T) {
	o := jsutil.NewObject()
	js.Global().Get("Reflect").Call("set", o, js.Global().Get("Symbol").Get("toStringTag"), "Deno.KvU64")
	got := jsToAny(o)
	if _, ok := got.(*KvU64); !ok {
		t.Errorf("jsToAny(KvU64-tagged) = %T, want *KvU64", got)
	}
}

func TestAnyToJS_Primitives(t *testing.T) {
	if got := anyToJS(nil); !got.IsNull() {
		t.Errorf("anyToJS(nil).IsNull() = false, want true")
	}
	if got := anyToJS("s"); got.String() != "s" {
		t.Errorf("anyToJS(\"s\") = %v, want \"s\"", got)
	}
	if got := anyToJS(1.5); got.Float() != 1.5 {
		t.Errorf("anyToJS(1.5) = %v, want 1.5", got)
	}
	if got := anyToJS(true); !got.Bool() {
		t.Errorf("anyToJS(true) = false, want true")
	}
}

// Values nested inside maps and slices are converted recursively: a []byte
// nested in a map[string]any becomes a Uint8Array instead of panicking in
// js.ValueOf ("invalid value").
func TestAnyToJS_Nested(t *testing.T) {
	now := time.UnixMilli(1700000000000).UTC()
	got := anyToJS(map[string]any{
		"bytes": []byte{1, 2},
		"inner": map[string]any{"n": 3},
		"list":  []any{"x", []byte{9}},
		"when":  now,
		"ss":    []string{"a", "b"},
	})
	bytesVal := got.Get("bytes")
	if !bytesVal.InstanceOf(jsutil.Uint8ArrayClass) || bytesVal.Length() != 2 || bytesVal.Index(0).Int() != 1 {
		t.Errorf("anyToJS(map)[\"bytes\"] = %v, want Uint8Array(2) [1 2]", bytesVal)
	}
	if n := got.Get("inner").Get("n").Float(); n != 3 {
		t.Errorf("nested map n = %v, want 3", n)
	}
	list := got.Get("list")
	if !list.Index(1).InstanceOf(jsutil.Uint8ArrayClass) || list.Index(1).Index(0).Int() != 9 {
		t.Errorf("nested list[1] = %v, want Uint8Array [9]", list.Index(1))
	}
	if d := got.Get("when"); !d.InstanceOf(jsutil.DateClass) {
		t.Errorf("nested time.Time = %v, want Date", d)
	}
	if ss := got.Get("ss"); ss.Length() != 2 || ss.Index(1).String() != "b" {
		t.Errorf("nested []string = %v, want [a b]", ss)
	}
}

func TestAnyToJS_RoundTrip(t *testing.T) {
	in := map[string]any{
		"s":     "v",
		"bytes": []byte{7, 8},
		"list":  []any{1.0, "two"},
	}
	out := jsToAny(anyToJS(in))
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("round trip = %T, want map[string]any", out)
	}
	if m["s"] != "v" || !equalBytes(m["bytes"].([]byte), []byte{7, 8}) || len(m["list"].([]any)) != 2 {
		t.Errorf("round trip = %v, want %v", m, in)
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
