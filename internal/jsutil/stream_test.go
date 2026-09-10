package jsutil

import "testing"

// panicReader is an io.Reader whose Read always panics, used to simulate a
// caller-supplied reader misbehaving inside ConvertReaderToReadableStream's
// pull executor.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("boom")
}

// TestConvertReaderToReadableStream_PullRecoversPanic verifies that a panic
// raised while pulling from the underlying io.Reader rejects the
// ReadableStreamDefaultReader's read() Promise instead of crashing the wasm
// program (which would leave the Promise pending forever and take the whole
// instance down with it).
func TestConvertReaderToReadableStream_PullRecoversPanic(t *testing.T) {
	stream := ConvertReaderToReadableStream(panicReader{})
	reader := stream.Call("getReader")

	// The stream's first pull (the "initialized" branch) just enqueues an
	// empty chunk without touching the reader, so the first read() resolves
	// normally. The panic surfaces on the second pull, which is the one that
	// actually calls panicReader.Read.
	if _, err := AwaitPromise(reader.Call("read")); err != nil {
		t.Fatalf("first read() returned error: %v", err)
	}
	if _, err := AwaitPromise(reader.Call("read")); err == nil {
		t.Fatal("second read() did not reject the Promise after Read panicked")
	}
}
