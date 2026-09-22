//go:build js && wasm

package r2

import (
	"io"
	"strings"
	"syscall/js"
	"testing"
	"time"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"

	"github.com/syumai/workers-go/internal/jsutil"
)

// TestToObject exercises toObject(r2ObjectLike, io.ReadCloser) against a
// *r2js.R2Object built from a raw js.Value shaped like the generated
// bindings expect (see exp/cloudflare/r2/zr2_gen.go's R2Object getters).
// R2HTTPMetadata's own decoding (undefined/null guards, Date conversion)
// is exercised by the generated package's own tests; here we only check
// that toObject wires HTTPMetadata/CustomMetadata/body through correctly,
// which replaces the former toHTTPMetadata- and single-arg-toObject-
// specific tests removed with the reimplementation on generated bindings.
func TestToObject(t *testing.T) {
	t.Run("full", func(t *testing.T) {
		uploaded := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		cacheExpiry := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
		v := js.ValueOf(map[string]any{
			"key":      "path/to/object",
			"version":  "v1",
			"size":     12,
			"etag":     "abc123",
			"httpEtag": `"abc123"`,
			"uploaded": jsutil.TimeToDate(uploaded),
			"httpMetadata": map[string]any{
				"contentType": "text/plain",
				"cacheExpiry": jsutil.TimeToDate(cacheExpiry),
			},
			"customMetadata": map[string]any{"foo": "bar"},
		})
		body := io.NopCloser(strings.NewReader("hello"))

		obj := toObject(r2js.R2ObjectFromJS(v), body)
		if obj.Key != "path/to/object" {
			t.Errorf("Key = %q, want %q", obj.Key, "path/to/object")
		}
		if obj.Version != "v1" {
			t.Errorf("Version = %q, want %q", obj.Version, "v1")
		}
		if obj.Size != 12 {
			t.Errorf("Size = %d, want 12", obj.Size)
		}
		if obj.ETag != "abc123" {
			t.Errorf("ETag = %q, want %q", obj.ETag, "abc123")
		}
		if obj.HTTPETag != `"abc123"` {
			t.Errorf("HTTPETag = %q, want %q", obj.HTTPETag, `"abc123"`)
		}
		if !obj.Uploaded.Equal(uploaded) {
			t.Errorf("Uploaded = %v, want %v", obj.Uploaded, uploaded)
		}
		if obj.HTTPMetadata.ContentType != "text/plain" {
			t.Errorf("HTTPMetadata.ContentType = %q, want %q", obj.HTTPMetadata.ContentType, "text/plain")
		}
		if !obj.HTTPMetadata.CacheExpiry.Equal(cacheExpiry) {
			t.Errorf("HTTPMetadata.CacheExpiry = %v, want %v", obj.HTTPMetadata.CacheExpiry, cacheExpiry)
		}
		if obj.CustomMetadata["foo"] != "bar" {
			t.Errorf("CustomMetadata[foo] = %q, want %q", obj.CustomMetadata["foo"], "bar")
		}
		if obj.Body != body {
			t.Fatalf("Body = %v, want the body passed to toObject", obj.Body)
		}
		got, err := io.ReadAll(obj.Body)
		if err != nil {
			t.Fatalf("io.ReadAll(Body): %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("Body content = %q, want %q", got, "hello")
		}
	})

	t.Run("metadata_undefined", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"key":      "k",
			"version":  "v1",
			"size":     0,
			"etag":     "e",
			"httpEtag": "e",
			"uploaded": jsutil.TimeToDate(time.Unix(0, 0)),
		})

		obj := toObject(r2js.R2ObjectFromJS(v), nil)
		if obj.HTTPMetadata != (HTTPMetadata{}) {
			t.Errorf("HTTPMetadata = %+v, want zero value", obj.HTTPMetadata)
		}
		if len(obj.CustomMetadata) != 0 {
			t.Errorf("CustomMetadata = %+v, want empty", obj.CustomMetadata)
		}
	})

	t.Run("body_nil_head_like", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"key":      "k",
			"version":  "v1",
			"size":     0,
			"etag":     "e",
			"httpEtag": "e",
			"uploaded": jsutil.TimeToDate(time.Unix(0, 0)),
		})

		obj := toObject(r2js.R2ObjectFromJS(v), nil)
		if obj.Body != nil {
			t.Errorf("Body = %v, want nil for a Head/Put-like object", obj.Body)
		}
	})
}

// TestPutOptions_toR2JS checks PutOptions.toR2JS(), which replaced the
// former js.Value-returning PutOptions.toJS()/HTTPMetadata.toJS() now that
// r2.PutOptions is converted into the generated r2js.R2PutOptions struct
// instead of a hand-built JS object. HTTPMetadata's own JS encoding
// (including the zero-CacheExpiry-omitted behavior the old
// TestHTTPMetadata_toJS checked) now lives entirely in the generated
// R2HTTPMetadata.toJS, outside this package.
func TestPutOptions_toR2JS(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		var opts *PutOptions
		got := opts.toR2JS()
		if got.HTTPMetadata != nil || got.CustomMetadata != nil || got.MD5 != "" {
			t.Errorf("toR2JS() = %+v, want zero value", got)
		}
	})

	t.Run("zero_value", func(t *testing.T) {
		got := (&PutOptions{}).toR2JS()
		if got.HTTPMetadata != nil || got.CustomMetadata != nil || got.MD5 != "" {
			t.Errorf("toR2JS() = %+v, want zero value", got)
		}
	})

	t.Run("http_metadata", func(t *testing.T) {
		opts := &PutOptions{HTTPMetadata: HTTPMetadata{ContentType: "text/plain"}}
		got := opts.toR2JS()
		if got.HTTPMetadata == nil {
			t.Fatalf("HTTPMetadata = nil, want a non-nil *R2HTTPMetadata")
		}
		if got.HTTPMetadata.ContentType != "text/plain" {
			t.Errorf("HTTPMetadata.ContentType = %q, want %q", got.HTTPMetadata.ContentType, "text/plain")
		}
	})

	t.Run("http_metadata_with_cache_expiry", func(t *testing.T) {
		expiry := time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC)
		opts := &PutOptions{HTTPMetadata: HTTPMetadata{CacheControl: "max-age=100", CacheExpiry: expiry}}
		got := opts.toR2JS()
		if got.HTTPMetadata == nil {
			t.Fatalf("HTTPMetadata = nil, want a non-nil *R2HTTPMetadata")
		}
		if !got.HTTPMetadata.CacheExpiry.Equal(expiry) {
			t.Errorf("HTTPMetadata.CacheExpiry = %v, want %v", got.HTTPMetadata.CacheExpiry, expiry)
		}
	})

	t.Run("custom_metadata", func(t *testing.T) {
		opts := &PutOptions{CustomMetadata: map[string]string{"foo": "bar", "baz": "qux"}}
		got := opts.toR2JS()
		if got.CustomMetadata["foo"] != "bar" || got.CustomMetadata["baz"] != "qux" {
			t.Errorf("CustomMetadata = %v, want {foo: bar, baz: qux}", got.CustomMetadata)
		}
	})

	t.Run("md5", func(t *testing.T) {
		opts := &PutOptions{MD5: "deadbeef"}
		got := opts.toR2JS()
		if got.MD5 != "deadbeef" {
			t.Errorf("MD5 = %q, want %q", got.MD5, "deadbeef")
		}
	})
}
