package jshttp

import (
	"io"
	"net/http"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

// withLenientResponseClass swaps jsutil.ResponseClass for a fake constructor
// that just records its (body, init) arguments as JS properties on a plain
// object, restoring the original on cleanup. Node's real Response
// constructor (used by the wasm test harness) rejects status 101 outright
// (RangeError: init["status"] must be in the range of 200 to 599), even
// though Cloudflare Workers' own Response class special-cases 101 for the
// WebSocket upgrade protocol. This lets tests assert on what newJSResponse
// passes to `new Response(body, init)` without needing a runtime that
// actually accepts a 101 Response.
func withLenientResponseClass(t *testing.T) {
	t.Helper()
	orig := jsutil.ResponseClass
	ctor := js.FuncOf(func(this js.Value, args []js.Value) any {
		obj := jsutil.NewObject()
		obj.Set("body", args[0])
		init := args[1]
		for _, k := range []string{"status", "statusText", "headers", "webSocket"} {
			obj.Set(k, init.Get(k))
		}
		return obj
	})
	jsutil.ResponseClass = ctor.Value
	t.Cleanup(func() {
		ctor.Release()
		jsutil.ResponseClass = orig
	})
}

// TestResponseWriter_ToJSResponse_WithoutWebSocket verifies that ToJSResponse
// behaves as before SetWebSocket existed: no webSocket property, and the
// status/body reflect WriteHeader/Write as usual.
func TestResponseWriter_ToJSResponse_WithoutWebSocket(t *testing.T) {
	reader, writer := io.Pipe()
	w := &ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
		Reader:      reader,
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}
	go func() {
		defer w.Ready()
		defer writer.Close()
		w.Write([]byte("hello"))
	}()
	<-w.ReadyCh

	resp := w.ToJSResponse()
	if got := resp.Get("status").Int(); got != http.StatusOK {
		t.Errorf("status = %d, want %d", got, http.StatusOK)
	}
	if got := resp.Get("webSocket"); !got.IsUndefined() {
		t.Errorf("webSocket = %v, want undefined", got)
	}
}

// closeTrackingReadCloser wraps an io.ReadCloser and records whether Close
// was called.
type closeTrackingReadCloser struct {
	io.ReadCloser
	closed bool
}

func (c *closeTrackingReadCloser) Close() error {
	c.closed = true
	return c.ReadCloser.Close()
}

// TestResponseWriter_ToJSResponse_NoContentClosesBody verifies that a
// bodyless response status (e.g. 204 No Content, which newJSResponse sends
// with a null body) still closes w.Reader, since that's the only thing that
// ever signals the caller (e.g. ServeRequest's onBodyClosed, wired up to
// close workers.Done() in handler_js.go) that the response is fully done.
// Without this, a handler that responds with WriteHeader(204) (and never
// otherwise closes the body) would hang workers.Serve forever.
func TestResponseWriter_ToJSResponse_NoContentClosesBody(t *testing.T) {
	reader, writer := io.Pipe()
	tracked := &closeTrackingReadCloser{ReadCloser: reader}
	w := &ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusNoContent,
		Reader:      tracked,
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}
	go func() {
		defer w.Ready()
		defer writer.Close()
	}()
	<-w.ReadyCh

	w.ToJSResponse()
	if !tracked.closed {
		t.Errorf("Reader was not closed for a %d response", http.StatusNoContent)
	}
}

// TestResponseWriter_ToJSResponse_WithWebSocket verifies that calling
// SetWebSocket forces status 101 and attaches the given js.Value as
// ResponseInit.webSocket, regardless of what WriteHeader set StatusCode to.
func TestResponseWriter_ToJSResponse_WithWebSocket(t *testing.T) {
	withLenientResponseClass(t)
	reader, writer := io.Pipe()
	tracked := &closeTrackingReadCloser{ReadCloser: reader}
	w := &ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
		Reader:      tracked,
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}
	ws := js.ValueOf(map[string]any{"tag": "fake-client-socket"})
	w.SetWebSocket(ws)

	go func() {
		defer w.Ready()
		defer writer.Close()
	}()
	<-w.ReadyCh

	resp := w.ToJSResponse()
	if got := resp.Get("status").Int(); got != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", got, http.StatusSwitchingProtocols)
	}
	if got := resp.Get("webSocket"); !got.Equal(ws) {
		t.Errorf("webSocket = %v, want %v", got, ws)
	}
	if got := resp.Get("body"); !got.IsNull() {
		t.Errorf("body = %v, want null", got)
	}
	if tracked.closed {
		t.Errorf("Reader was closed for a WebSocket response, want left open so onBodyClosed doesn't fire before the handler's goroutines (e.g. an echo loop) run")
	}
}
