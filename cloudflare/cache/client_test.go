//go:build js && wasm

package cache

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"syscall/js"
	"testing"
)

func newTestRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	return req
}

func newTestResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestNew_default(t *testing.T) {
	fc := installFakeCaches(t)

	c := New()
	if err := c.Put(newTestRequest(t, "https://example.com/foo"), newTestResponse("hello")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if got := fc.def.entryCount(); got != 1 {
		t.Errorf("default cache entryCount() = %d, want 1", got)
	}
	if got := fc.namespace("ns").entryCount(); got != 0 {
		t.Errorf("namespace(\"ns\") entryCount() = %d, want 0 (New() without options must not touch it)", got)
	}
}

func TestNew_withNamespace(t *testing.T) {
	fc := installFakeCaches(t)

	c := New(WithNamespace("ns"))
	if err := c.Put(newTestRequest(t, "https://example.com/foo"), newTestResponse("hello")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if got := fc.namespace("ns").entryCount(); got != 1 {
		t.Errorf("namespace(\"ns\") entryCount() = %d, want 1", got)
	}
	if got := fc.def.entryCount(); got != 0 {
		t.Errorf("default cache entryCount() = %d, want 0", got)
	}
}

func TestCache_Put_Match_Delete(t *testing.T) {
	installFakeCaches(t)
	c := New()

	url := "https://example.com/foo"
	if err := c.Put(newTestRequest(t, url), newTestResponse("hello")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	res, err := c.Match(newTestRequest(t, url), nil)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("Match() StatusCode = %d, want %d", res.StatusCode, http.StatusOK)
	}

	if err := c.Delete(newTestRequest(t, url), nil); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := c.Match(newTestRequest(t, url), nil); !errors.Is(err, ErrCacheNotFound) {
		t.Fatalf("Match() after Delete() error = %v, want %v", err, ErrCacheNotFound)
	}

	// Deleting an already-deleted entry reports ErrCacheNotFound.
	if err := c.Delete(newTestRequest(t, url), nil); !errors.Is(err, ErrCacheNotFound) {
		t.Fatalf("Delete() (2nd) error = %v, want %v", err, ErrCacheNotFound)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("io.ReadAll(res.Body) error = %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}
}

// TestCache_Match_miss fixes the current behavior for a cache miss: the real
// Cache API resolves match() with undefined, and Cache.Match turns that into
// ErrCacheNotFound rather than a nil, nil result.
func TestCache_Match_miss(t *testing.T) {
	installFakeCaches(t)
	c := New()

	_, err := c.Match(newTestRequest(t, "https://example.com/missing"), nil)
	if !errors.Is(err, ErrCacheNotFound) {
		t.Fatalf("Match() error = %v, want %v", err, ErrCacheNotFound)
	}
}

func TestMatch_ignoreMethod(t *testing.T) {
	fc := installFakeCaches(t)
	c := New()

	if _, err := c.Match(newTestRequest(t, "https://example.com/foo"), &MatchOptions{IgnoreMethod: true}); err != nil && !errors.Is(err, ErrCacheNotFound) {
		t.Fatalf("Match() error = %v", err)
	}

	opts := fc.def.lastMatchOpts
	if opts.IsUndefined() {
		t.Fatalf("match() was not called with an options object")
	}
	if got := opts.Get("ignoreMethod").Bool(); !got {
		t.Errorf("match() options.ignoreMethod = %v, want true", got)
	}
}

// TestNew_undefinedCaches fixes the current behavior when the global caches
// object is undefined: New() calls cachejs.Caches().Default(), which calls
// js.Value.Get on the underlying (undefined) JS value and panics with a JS
// exception rather than a plain string.
func TestNew_undefinedCaches(t *testing.T) {
	prev := js.Global().Get("caches")
	js.Global().Set("caches", js.Undefined())
	t.Cleanup(func() { js.Global().Set("caches", prev) })

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("New() did not panic")
		}
	}()
	New()
}
