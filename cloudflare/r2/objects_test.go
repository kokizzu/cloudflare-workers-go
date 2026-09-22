//go:build js && wasm

package r2

import (
	"slices"
	"syscall/js"
	"testing"
	"time"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"

	"github.com/syumai/workers-go/internal/jsutil"
)

func TestToObjects(t *testing.T) {
	uploaded := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	obj := func(key string, size int) *r2js.R2Object {
		v := js.ValueOf(map[string]any{
			"key":      key,
			"version":  "v1",
			"size":     size,
			"etag":     "e-" + key,
			"httpEtag": "e-" + key,
			"uploaded": jsutil.TimeToDate(uploaded),
		})
		return r2js.R2ObjectFromJS(v)
	}

	in := r2js.R2Objects{
		Objects:           []*r2js.R2Object{obj("a", 1), obj("b", 2)},
		Truncated:         true,
		Cursor:            "next-cursor",
		DelimitedPrefixes: []string{"a/", "b/"},
	}

	got := toObjects(in)
	if len(got.Objects) != 2 {
		t.Fatalf("len(Objects) = %d, want 2", len(got.Objects))
	}
	if got.Objects[0].Key != "a" || got.Objects[0].Size != 1 {
		t.Errorf("Objects[0] = %+v, want Key=a Size=1", got.Objects[0])
	}
	if got.Objects[1].Key != "b" || got.Objects[1].Size != 2 {
		t.Errorf("Objects[1] = %+v, want Key=b Size=2", got.Objects[1])
	}
	if !got.Truncated {
		t.Errorf("Truncated = false, want true")
	}
	if got.Cursor != "next-cursor" {
		t.Errorf("Cursor = %q, want %q", got.Cursor, "next-cursor")
	}
	if !slices.Equal(got.DelimitedPrefixes, []string{"a/", "b/"}) {
		t.Errorf("DelimitedPrefixes = %v, want [a/ b/]", got.DelimitedPrefixes)
	}
}

func TestToObjects_cursorMissing(t *testing.T) {
	in := r2js.R2Objects{
		Objects:           nil,
		Truncated:         false,
		DelimitedPrefixes: nil,
	}

	got := toObjects(in)
	if len(got.Objects) != 0 {
		t.Errorf("len(Objects) = %d, want 0", len(got.Objects))
	}
	if got.Truncated {
		t.Errorf("Truncated = true, want false")
	}
	if got.Cursor != "" {
		t.Errorf("Cursor = %q, want empty", got.Cursor)
	}
	if len(got.DelimitedPrefixes) != 0 {
		t.Errorf("DelimitedPrefixes = %v, want empty", got.DelimitedPrefixes)
	}
}
