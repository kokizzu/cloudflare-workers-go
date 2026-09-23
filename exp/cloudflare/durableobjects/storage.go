//go:build js && wasm

package durableobjects

import (
	"encoding/json"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// GetString reads key as a string. The second return value reports whether
// the key was present; if it wasn't, the returned string is the empty
// string and no error is returned.
func (s *DurableObjectStorage) GetString(key string) (string, bool, error) {
	v, err := s.Get(key, DurableObjectGetOptions{})
	if err != nil {
		return "", false, err
	}
	if jsrt.IsNil(v) {
		return "", false, nil
	}
	return v.String(), true, nil
}

// PutString stores value as a string under key.
func (s *DurableObjectStorage) PutString(key, value string) error {
	return s.Put(key, js.ValueOf(value), DurableObjectPutOptions{})
}

// GetJSON reads key and JSON-decodes it into v, which must be a pointer (as
// for [json.Unmarshal]). It bridges through the JS side's JSON.stringify,
// re-encoding the stored structured-clonable value as text before handing it
// to [encoding/json.Unmarshal], since [DurableObjectStorage] itself has no
// notion of a Go type to decode into. The second return value reports
// whether the key was present; if it wasn't, v is left untouched and no
// error is returned.
func (s *DurableObjectStorage) GetJSON(key string, v any) (bool, error) {
	raw, err := s.Get(key, DurableObjectGetOptions{})
	if err != nil {
		return false, err
	}
	if jsrt.IsNil(raw) {
		return false, nil
	}
	stringified, err := jsrt.Call(js.Global().Get("JSON"), "stringify", raw)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(stringified.String()), v); err != nil {
		return false, err
	}
	return true, nil
}

// PutJSON JSON-encodes v (via [encoding/json.Marshal]) and stores the result
// under key. The encoded JSON text is parsed back into a JS value
// (JSON.parse) before being stored, so it round-trips through GetJSON (or
// DurableObjectStorage's own list/get) as a structured value rather than a
// raw JSON string.
func (s *DurableObjectStorage) PutJSON(key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	parsed, err := jsrt.Call(js.Global().Get("JSON"), "parse", string(b))
	if err != nil {
		return err
	}
	return s.Put(key, parsed, DurableObjectPutOptions{})
}

// Alarm returns the currently scheduled alarm time. The second return value
// reports whether an alarm is scheduled; if not, the returned time is the
// zero value and no error is returned.
func (s *DurableObjectStorage) Alarm() (time.Time, bool, error) {
	ms, err := s.GetAlarm(DurableObjectGetAlarmOptions{})
	if err != nil {
		return time.Time{}, false, err
	}
	if ms == nil {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(int64(*ms)), true, nil
}

// Rows decodes the cursor's remaining rows (via ToArray) into a slice of
// column name -> value maps, one per row. Each column value — a
// SqlStorageValue (string | number | null | ArrayBuffer) on the JS side —
// decodes to a string, float64, nil, or []byte respectively.
func (c *SQLStorageCursor) Rows() ([]map[string]any, error) {
	values, err := c.ToArray()
	if err != nil {
		return nil, err
	}
	names := c.ColumnNames()
	rows := make([]map[string]any, len(values))
	for i, rowVal := range values {
		row := make(map[string]any, len(names))
		for _, name := range names {
			row[name] = decodeSQLStorageValue(rowVal.Get(name))
		}
		rows[i] = row
	}
	return rows, nil
}

// decodeSQLStorageValue decodes one SqlStorageValue (string | number | null
// | ArrayBuffer) column value.
func decodeSQLStorageValue(v js.Value) any {
	if jsrt.IsNil(v) {
		return nil
	}
	switch v.Type() {
	case js.TypeString:
		return v.String()
	case js.TypeNumber:
		return v.Float()
	default:
		// The only remaining SqlStorageValue case is ArrayBuffer. Unlike
		// Uint8Array (which jsrt.BytesFromJS/syscall/js's CopyBytesToGo
		// handle directly), a raw ArrayBuffer isn't itself a typed-array
		// view, so wrap it in one first.
		return jsrt.BytesFromJS(js.Global().Get("Uint8Array").New(v))
	}
}
