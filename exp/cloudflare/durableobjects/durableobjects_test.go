//go:build js && wasm

package durableobjects

import (
	"syscall/js"
	"testing"
	"time"
)

func resolvedPromise(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestDurableObjectStorage_Get_Missing exercises the Get/GetString round
// trip when the underlying JS storage.get resolves to undefined (no such
// key): Get itself should report that with IsNil, and GetString should
// translate it into its own "not found" bool rather than the string
// "undefined".
func TestDurableObjectStorage_Get_Missing(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotKey string
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotKey = args[0].String()
		return resolvedPromise(js.Undefined())
	}))

	s := DurableObjectStorageFromJS(fake)
	v, err := s.Get("missing", DurableObjectGetOptions{})
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if !v.IsUndefined() {
		t.Fatalf("Get() = %v, want undefined", v)
	}
	if gotKey != "missing" {
		t.Fatalf("key sent to JS = %q, want %q", gotKey, "missing")
	}

	str, ok, err := s.GetString("missing")
	if err != nil {
		t.Fatalf("GetString() failed: %v", err)
	}
	if ok {
		t.Fatalf("GetString() ok = true, want false")
	}
	if str != "" {
		t.Fatalf("GetString() = %q, want empty string", str)
	}
}

// TestDurableObjectStorage_Put exercises Put and PutString: the JS side's
// storage.put(key, value, options) receives the key and the exact value
// (unwrapped from the js.Value/string argument), and the returned Promise's
// resolution surfaces as a nil error.
func TestDurableObjectStorage_Put(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotKey, gotValue string
	fake.Set("put", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotKey = args[0].String()
		gotValue = args[1].String()
		return resolvedPromise(js.Undefined())
	}))

	s := DurableObjectStorageFromJS(fake)
	if err := s.PutString("greeting", "hello"); err != nil {
		t.Fatalf("PutString() failed: %v", err)
	}
	if gotKey != "greeting" || gotValue != "hello" {
		t.Fatalf("put(%q, %q), want put(%q, %q)", gotKey, gotValue, "greeting", "hello")
	}
}

// TestDurableObjectStorage_List exercises the Map<string, T> decoding added
// for tmp/06-codegen-spec.md 4.1: the fake storage.list resolves with a real
// JS Map (as the actual runtime would), and List must decode it into a Go
// map[string]js.Value via Array.from(keys())/get(k), not Object.keys (which
// doesn't see a Map's entries).
func TestDurableObjectStorage_List(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("list", js.FuncOf(func(this js.Value, args []js.Value) any {
		m := js.Global().Get("Map").New()
		m.Call("set", "a", "1")
		m.Call("set", "b", "2")
		return resolvedPromise(m)
	}))

	s := DurableObjectStorageFromJS(fake)
	got, err := s.List(DurableObjectListOptions{})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List() returned %d entries, want 2 (%v)", len(got), got)
	}
	if v, ok := got["a"]; !ok || v.String() != "1" {
		t.Errorf(`List()["a"] = %v, %v, want "1", true`, v, ok)
	}
	if v, ok := got["b"]; !ok || v.String() != "2" {
		t.Errorf(`List()["b"] = %v, %v, want "2", true`, v, ok)
	}
}

// TestDurableObjectStorage_SetAlarm verifies that the scheduledTime
// parameter — a "number | Date" union in the .d.ts, forced to time.Time via
// a types: override per tmp/06-codegen-spec.md 4.1 — is actually sent to JS
// as a Date instance (the "Date side" of the union), not a plain number.
func TestDurableObjectStorage_SetAlarm(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	var gotIsDate bool
	var gotMs float64
	fake.Set("setAlarm", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotIsDate = args[0].InstanceOf(js.Global().Get("Date"))
		gotMs = args[0].Call("getTime").Float()
		return resolvedPromise(js.Undefined())
	}))

	s := DurableObjectStorageFromJS(fake)
	when := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := s.SetAlarm(when, DurableObjectSetAlarmOptions{}); err != nil {
		t.Fatalf("SetAlarm() failed: %v", err)
	}
	if !gotIsDate {
		t.Fatalf("setAlarm's scheduledTime arg is not a JS Date instance")
	}
	if want := float64(when.UnixMilli()); gotMs != want {
		t.Fatalf("setAlarm's scheduledTime = %v ms, want %v ms", gotMs, want)
	}
}

// TestDurableObjectStorage_GetJSON_PutJSON round-trips a Go struct through
// PutJSON/GetJSON against a fake storage backed by a plain Go map, verifying
// the JSON.stringify/JSON.parse bridge documented on both methods.
func TestDurableObjectStorage_GetJSON_PutJSON(t *testing.T) {
	type record struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	store := map[string]js.Value{}
	fake := js.ValueOf(map[string]any{})
	fake.Set("put", js.FuncOf(func(this js.Value, args []js.Value) any {
		store[args[0].String()] = args[1]
		return resolvedPromise(js.Undefined())
	}))
	fake.Set("get", js.FuncOf(func(this js.Value, args []js.Value) any {
		v, ok := store[args[0].String()]
		if !ok {
			return resolvedPromise(js.Undefined())
		}
		return resolvedPromise(v)
	}))

	s := DurableObjectStorageFromJS(fake)
	want := record{Name: "Ada", Age: 36}
	if err := s.PutJSON("person", want); err != nil {
		t.Fatalf("PutJSON() failed: %v", err)
	}

	var got record
	ok, err := s.GetJSON("person", &got)
	if err != nil {
		t.Fatalf("GetJSON() failed: %v", err)
	}
	if !ok {
		t.Fatalf("GetJSON() ok = false, want true")
	}
	if got != want {
		t.Fatalf("GetJSON() = %+v, want %+v", got, want)
	}

	var missing record
	if ok, err := s.GetJSON("does-not-exist", &missing); err != nil || ok {
		t.Fatalf("GetJSON(\"does-not-exist\") = %v, %v, want false, nil", ok, err)
	}
}

// TestSQLStorageCursor_Rows exercises the storage.go Rows helper against a
// fake cursor whose toArray()/columnNames mimic SqlStorage.exec's real
// shape: an array of plain row objects keyed by column name, with values
// covering every SqlStorageValue case (string, number, null, ArrayBuffer).
func TestSQLStorageCursor_Rows(t *testing.T) {
	fake := js.ValueOf(map[string]any{})
	fake.Set("columnNames", js.ValueOf([]any{"name", "count", "note", "blob"}))
	row := js.ValueOf(map[string]any{})
	row.Set("name", "widget")
	row.Set("count", 3)
	row.Set("note", nil)
	buf := js.Global().Get("Uint8Array").New(3)
	js.CopyBytesToJS(buf, []byte{1, 2, 3})
	row.Set("blob", buf.Get("buffer"))
	fake.Set("toArray", js.FuncOf(func(this js.Value, args []js.Value) any {
		return js.Global().Get("Array").New(1).Call("fill", row)
	}))

	c := SQLStorageCursorFromJS(fake)
	rows, err := c.Rows()
	if err != nil {
		t.Fatalf("Rows() failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Rows() returned %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got["name"] != "widget" {
		t.Errorf(`Rows()[0]["name"] = %v, want "widget"`, got["name"])
	}
	if got["count"] != float64(3) {
		t.Errorf(`Rows()[0]["count"] = %v, want 3`, got["count"])
	}
	if got["note"] != nil {
		t.Errorf(`Rows()[0]["note"] = %v, want nil`, got["note"])
	}
	blob, ok := got["blob"].([]byte)
	if !ok {
		t.Fatalf(`Rows()[0]["blob"] has type %T, want []byte`, got["blob"])
	}
	if string(blob) != "\x01\x02\x03" {
		t.Errorf(`Rows()[0]["blob"] = %v, want [1 2 3]`, blob)
	}
}
