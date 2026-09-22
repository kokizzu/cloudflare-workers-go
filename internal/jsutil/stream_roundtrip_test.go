//go:build js && wasm

package jsutil

import (
	"bytes"
	"io"
	"syscall/js"
	"testing"
)

// readAllStream reads a JS ReadableStream to completion using
// ConvertReadableStreamToReadCloser, the function under test in some of the
// cases below.
func readAllStream(t *testing.T, stream js.Value) []byte {
	t.Helper()
	rc := ConvertReadableStreamToReadCloser(stream)
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("readAllStream: %v", err)
	}
	return b
}

// fakeReadableStream builds a ReadableStream that enqueues chunks in order
// and then closes. It is a local copy of internal/jstest.ReadableStream:
// internal/jstest imports this package, so importing it back here would
// create an import cycle.
func fakeReadableStream(chunks ...[]byte) js.Value {
	init := NewObject()
	var start js.Func
	start = js.FuncOf(func(_ js.Value, args []js.Value) any {
		controller := args[0]
		for _, c := range chunks {
			ua := NewUint8Array(len(c))
			if len(c) > 0 {
				js.CopyBytesToJS(ua, c)
			}
			controller.Call("enqueue", ua)
		}
		controller.Call("close")
		return js.Undefined()
	})
	init.Set("start", start)
	rs := ReadableStreamClass.New(init)
	start.Release()
	return rs
}

func TestConvertReaderToReadableStream_ReadAll(t *testing.T) {
	sizes := map[string]int{
		"empty":              0,
		"one_byte":           1,
		"under_chunk_size":   defaultChunkSize - 1,
		"multiple_of_chunks": defaultChunkSize*3 + 7,
	}
	for name, size := range sizes {
		t.Run(name, func(t *testing.T) {
			want := make([]byte, size)
			for i := range want {
				want[i] = byte(i % 251)
			}

			stream := ConvertReaderToReadableStream(io.NopCloser(bytes.NewReader(want)))
			got := readAllStream(t, stream)
			if !bytes.Equal(got, want) {
				t.Errorf("ReadAll() = %d bytes, want %d bytes (content mismatch)", len(got), len(want))
			}
		})
	}
}

func TestConvertReadableStreamToReadCloser_ReadAll(t *testing.T) {
	tests := map[string][][]byte{
		"empty":       {},
		"one_chunk":   {[]byte("hello")},
		"three_chunk": {[]byte("foo"), []byte("bar"), []byte("baz")},
	}
	for name, chunks := range tests {
		t.Run(name, func(t *testing.T) {
			var want []byte
			for _, c := range chunks {
				want = append(want, c...)
			}

			got := readAllStream(t, fakeReadableStream(chunks...))
			if !bytes.Equal(got, want) {
				t.Errorf("ReadAll() = %q, want %q", got, want)
			}
		})
	}
}

func TestStream_roundTrip(t *testing.T) {
	want := bytes.Repeat([]byte("round trip "), 1000)

	stream := ConvertReaderToReadableStream(io.NopCloser(bytes.NewReader(want)))
	rc := ConvertReadableStreamToReadCloser(stream)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v, want nil", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("round trip mismatch: got %d bytes, want %d bytes", len(got), len(want))
	}
}

func TestReadableStreamToReadCloser_Close_cancels(t *testing.T) {
	canceled := false
	init := NewObject()
	var start, cancelFn js.Func
	start = js.FuncOf(func(_ js.Value, args []js.Value) any {
		controller := args[0]
		// Enqueue a non-empty chunk so the first Read below returns data
		// immediately; cancel-on-close behavior is what this test
		// exercises, not empty-chunk handling.
		controller.Call("enqueue", NewUint8Array(1))
		return js.Undefined()
	})
	cancelFn = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		canceled = true
		return js.Undefined()
	})
	defer start.Release()
	defer cancelFn.Release()
	init.Set("start", start)
	init.Set("cancel", cancelFn)
	stream := ReadableStreamClass.New(init)

	rc := ConvertReadableStreamToReadCloser(stream)
	// Read once so that Close (which cancels the lazily created reader) has
	// something to cancel.
	buf := make([]byte, 1)
	if _, err := rc.Read(buf); err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
	if !canceled {
		t.Errorf("the ReadableStream's cancel callback was not invoked after Close")
	}
}

type fakeRawBodyWriter struct {
	body js.Value
}

func (w *fakeRawBodyWriter) Write(p []byte) (int, error)  { return len(p), nil }
func (w *fakeRawBodyWriter) WriteRawJSBody(body js.Value) { w.body = body }

func TestReadableStreamToReadCloser_WriteTo_rawJSBody(t *testing.T) {
	stream := fakeReadableStream([]byte("hello"))
	rc := ConvertReadableStreamToReadCloser(stream)
	defer rc.Close()

	wt, ok := rc.(io.WriterTo)
	if !ok {
		t.Fatalf("ConvertReadableStreamToReadCloser() result does not implement io.WriterTo")
	}

	w := &fakeRawBodyWriter{}
	n, err := wt.WriteTo(w)
	if err != nil {
		t.Fatalf("WriteTo() error = %v, want nil", err)
	}
	if n != 0 {
		t.Errorf("WriteTo() copied %d bytes, want 0 (the raw stream should be passed through instead)", n)
	}
	if !w.body.Equal(stream) {
		t.Errorf("WriteRawJSBody() got %v, want the original stream %v", w.body, stream)
	}
}

type countingReadCloser struct {
	io.Reader
	closes int
}

func (c *countingReadCloser) Close() error {
	c.closes++
	return nil
}

func TestReaderToReadableStream_cancel_closesReader(t *testing.T) {
	src := &countingReadCloser{Reader: bytes.NewReader(nil)}
	stream := ConvertReaderToReadableStream(src)

	if _, err := AwaitPromise(stream.Call("cancel")); err != nil {
		t.Fatalf("cancel() promise rejected: %v", err)
	}
	if src.closes != 1 {
		t.Errorf("reader Close() called %d times, want 1", src.closes)
	}
}

func TestConvertReaderToFixedLengthStream(t *testing.T) {
	t.Skip("FixedLengthStream is not available under Node; verified via wrangler e2e (see tmp/test-plan/04-wrangler-e2e.md)")
}
