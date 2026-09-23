//go:build js && wasm

package jshttp

import (
	"io"
	"net/http"
	"testing"
)

func newTestResponseWriter() (*ResponseWriter, *io.PipeWriter) {
	reader, writer := io.Pipe()
	return &ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
		Reader:      reader,
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}, writer
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func TestResponseWriter_Ready(t *testing.T) {
	// NOTE: unlike the 02-unit-test-catalog.md wording ("WriteHeader closes
	// ReadyCh"), the current implementation's WriteHeader only records the
	// status code (see responsewriter.go) - it never calls Ready. Only an
	// explicit Ready() call (or Write, see TestResponseWriter_WriteImpliesReady)
	// closes ReadyCh. This test pins that actual behavior.
	w, writer := newTestResponseWriter()
	defer writer.Close()

	if isClosed(w.ReadyCh) {
		t.Fatalf("ReadyCh is already closed before Ready/Write is called")
	}

	w.WriteHeader(http.StatusTeapot)
	if isClosed(w.ReadyCh) {
		t.Errorf("ReadyCh was closed by WriteHeader alone; current behavior only closes it via Ready or Write")
	}

	w.Ready()
	if !isClosed(w.ReadyCh) {
		t.Fatalf("ReadyCh is not closed after Ready()")
	}

	// Calling Ready again must not panic (sync.Once).
	w.Ready()
}

func TestResponseWriter_WriteImpliesReady(t *testing.T) {
	w, writer := newTestResponseWriter()

	if isClosed(w.ReadyCh) {
		t.Fatalf("ReadyCh is already closed before Write is called")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Write([]byte("body"))
	}()
	defer func() {
		writer.Close()
		<-done
	}()

	<-w.ReadyCh // Write must close ReadyCh before/without an explicit WriteHeader.
}

func TestResponseWriter_HeaderSnapshot(t *testing.T) {
	w, writer := newTestResponseWriter()
	defer writer.Close()

	w.Header().Set("X-Before", "1")
	w.WriteHeader(http.StatusOK)
	// NOTE (current behavior, differs from net/http): net/http snapshots
	// (and starts sending) headers as soon as WriteHeader is called, so
	// mutating them afterward has no effect on the response actually sent.
	// This ResponseWriter does not snapshot anything at WriteHeader time:
	// ToJSResponse reads w.HeaderValue lazily, whenever it is called. So a
	// Header().Set call issued after WriteHeader (but before ToJSResponse)
	// still takes effect, as asserted below via X-After.
	w.Header().Set("X-After", "2")
	w.Ready() // mark ready without needing to write/read a body

	resp := w.ToJSResponse()
	if got := resp.Get("headers").Call("get", "X-Before").String(); got != "1" {
		t.Errorf("headers.get(X-Before) = %q, want %q", got, "1")
	}
	if got := resp.Get("headers").Call("get", "X-After").String(); got != "2" {
		t.Errorf("headers.get(X-After) = %q, want %q (a header set after WriteHeader should still be reflected, unlike net/http)", got, "2")
	}
}

func TestResponseWriter_ToJSResponse(t *testing.T) {
	w, writer := newTestResponseWriter()

	w.Header().Set("X-Test", "1")
	w.WriteHeader(http.StatusCreated)

	// Drive the body through the pipe: ToJSResponse exposes w.Reader as a
	// ReadableStream, and readAllStream below drains it while this
	// goroutine feeds it.
	go func() {
		defer writer.Close()
		if _, err := writer.Write([]byte("response body")); err != nil {
			t.Errorf("pipe Write: %v", err)
		}
	}()

	resp := w.ToJSResponse()
	if got := resp.Get("status").Int(); got != http.StatusCreated {
		t.Errorf("status = %d, want %d", got, http.StatusCreated)
	}
	if got := resp.Get("headers").Call("get", "X-Test").String(); got != "1" {
		t.Errorf("headers.get(X-Test) = %q, want %q", got, "1")
	}
	if got := readAllStream(t, resp.Get("body")); string(got) != "response body" {
		t.Errorf("body = %q, want %q", got, "response body")
	}
}

func TestResponseWriter_Flush_noop(t *testing.T) {
	w, writer := newTestResponseWriter()
	defer writer.Close()

	w.Flush() // must not panic
}
