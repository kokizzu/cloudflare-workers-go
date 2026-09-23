//go:build js && wasm

// Package deno provides Go bindings for the Deno runtime APIs (the `Deno`
// global namespace), generated from the output of `deno types` /
// `deno doc --json` by scripts/gen-deno.
//
// This package is experimental.
package deno

import (
	"reflect"
	"strconv"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// deno is the Deno global namespace object.
var deno = js.Global().Get("Deno")

// jsConverter is implemented by generated struct types that can convert
// themselves to a JavaScript object.
type jsConverter interface {
	toJS() js.Value
}

func isNullish(v js.Value) bool {
	return v.IsNull() || v.IsUndefined()
}

func getString(v js.Value) string {
	if isNullish(v) {
		return ""
	}
	return v.String()
}

func getFloat(v js.Value) float64 {
	if isNullish(v) {
		return 0
	}
	return v.Float()
}

func getBool(v js.Value) bool {
	return !isNullish(v) && v.Bool()
}

// protoToString is Object.prototype.toString. js.Value.Type() cannot be
// used to classify values here: it panics on types the wasm ABI has no flag
// for (BigInt), and it cannot distinguish plain objects from class instances
// anyway (a Deno.KvU64 is instanceof Object, so checking instanceof would
// wrongly convert it to a map and then panic on its BigInt field).
var protoToString = jsutil.ObjectClass.Get("prototype").Get("toString")

func typeTag(v js.Value) string {
	return protoToString.Call("call", v).String()
}

// jsToAny converts a JavaScript value to a Go value. Plain objects become
// map[string]any, arrays become []any, Uint8Array becomes []byte, Date
// becomes time.Time, KvU64 becomes *KvU64, and other JS values (class
// instances, functions, symbols, BigInts, ...) are passed through as
// js.Value.
func jsToAny(v js.Value) any {
	if isNullish(v) {
		return nil
	}
	switch typeTag(v) {
	case "[object Boolean]":
		return v.Bool()
	case "[object Number]":
		return v.Float()
	case "[object String]":
		return v.String()
	case "[object Uint8Array]", "[object Uint8ClampedArray]":
		return uint8ArrayToBytes(v)
	case "[object Array]":
		n := v.Length()
		out := make([]any, n)
		for i := 0; i < n; i++ {
			out[i] = jsToAny(v.Index(i))
		}
		return out
	case "[object Date]":
		t, _ := jsutil.DateToTime(v)
		return t
	// Deno.KvU64 instances are tagged "Deno.KvU64" by Symbol.toStringTag;
	// "KvU64" is kept for compatibility with older/other shims.
	case "[object Deno.KvU64]", "[object KvU64]":
		return kvU64FromJS(v)
	case "[object Object]":
		m := make(map[string]any)
		keys := jsutil.ObjectClass.Call("keys", v)
		for i := 0; i < keys.Length(); i++ {
			k := keys.Index(i).String()
			m[k] = jsToAny(v.Get(k))
		}
		return m
	default:
		return v
	}
}

// anyToJS converts a Go value to a JavaScript value. Generated struct types
// (which implement jsConverter), []byte, time.Time and js.Value are
// converted explicitly. Slices/arrays become JavaScript Arrays and maps
// with string keys become plain objects, with each element converted
// recursively so that e.g. a []byte nested inside a map[string]any still
// becomes a Uint8Array. Other values go through js.ValueOf, which supports
// booleans, numbers and strings.
func anyToJS(v any) js.Value {
	switch x := v.(type) {
	case nil:
		return js.Null()
	case js.Value:
		return x
	case jsConverter:
		return x.toJS()
	case []byte:
		return bytesToUint8Array(x)
	case time.Time:
		return jsutil.TimeToDate(x)
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			arr := jsutil.NewArray(rv.Len())
			for i := 0; i < rv.Len(); i++ {
				arr.SetIndex(i, anyToJS(rv.Index(i).Interface()))
			}
			return arr
		case reflect.Map:
			if rv.Type().Key().Kind() != reflect.String {
				break
			}
			o := jsutil.NewObject()
			iter := rv.MapRange()
			for iter.Next() {
				o.Set(iter.Key().String(), anyToJS(iter.Value().Interface()))
			}
			return o
		}
		return js.ValueOf(v)
	}
}

// sliceToJS converts a Go slice to a JavaScript Array using conv for each
// element.
func sliceToJS[T any](s []T, conv func(T) js.Value) js.Value {
	arr := jsutil.NewArray(len(s))
	for i, e := range s {
		arr.SetIndex(i, conv(e))
	}
	return arr
}

// sliceFromJS converts a JavaScript Array to a Go slice using conv for each
// element. Nullish input yields nil.
func sliceFromJS[T any](v js.Value, conv func(js.Value) T) []T {
	if isNullish(v) {
		return nil
	}
	n := v.Length()
	out := make([]T, n)
	for i := 0; i < n; i++ {
		out[i] = conv(v.Index(i))
	}
	return out
}

// ptrFromJS converts a JavaScript value to *T using conv. Nullish input yields
// nil.
func ptrFromJS[T any](v js.Value, conv func(js.Value) T) *T {
	if isNullish(v) {
		return nil
	}
	x := conv(v)
	return &x
}

// ptrToJS converts *T to a JavaScript value using conv. Nil input yields
// undefined.
func ptrToJS[T any](p *T, conv func(T) js.Value) js.Value {
	if p == nil {
		return js.Undefined()
	}
	return conv(*p)
}

// strMapToJS converts a Go map[string]string into a JavaScript object.
func strMapToJS(m map[string]string) js.Value {
	if m == nil {
		return js.Undefined()
	}
	o := jsutil.NewObject()
	for k, v := range m {
		o.Set(k, v)
	}
	return o
}

// uint8ArrayToBytes copies a JavaScript Uint8Array into a Go byte slice.
func uint8ArrayToBytes(v js.Value) []byte {
	if isNullish(v) {
		return nil
	}
	b := make([]byte, v.Length())
	js.CopyBytesToGo(b, v)
	return b
}

// bytesToUint8Array copies a Go byte slice into a new JavaScript Uint8Array.
func bytesToUint8Array(b []byte) js.Value {
	if b == nil {
		return js.Null()
	}
	arr := jsutil.Uint8ArrayClass.New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}

func dateToTime(v js.Value) time.Time {
	t, _ := jsutil.DateToTime(v)
	return t
}

// BigInt returns a JavaScript BigInt for the given uint64 value. It is needed
// for APIs that take bigint arguments, such as NewKvU64.
func BigInt(u uint64) js.Value {
	return js.Global().Get("BigInt").Invoke(strconv.FormatUint(u, 10))
}

// bigIntToUint64 converts a JavaScript BigInt into a uint64. v.Call cannot
// be used on a BigInt: Value.Call checks v.Type(), which panics because
// BigInt has no wasm ABI type flag. String(v) handles the conversion in JS.
func bigIntToUint64(v js.Value) uint64 {
	if isNullish(v) {
		return 0
	}
	u, _ := strconv.ParseUint(js.Global().Get("String").Invoke(v).String(), 10, 64)
	return u
}
