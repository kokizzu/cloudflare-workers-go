//go:build js && wasm

package images

import (
	"strings"
	"syscall/js"
	"testing"
)

// TestImagesBinding_Input_PassesReadableStream verifies that Input converts
// its io.Reader argument into a JS ReadableStream (the parameter-position
// mapping added by cfgen for ReadableStream<...>, per
// tmp/06-codegen-spec.md 3.1.1), and that the transform chain returns the
// same underlying handle (the fake's transform() returns `this`).
func TestImagesBinding_Input_PassesReadableStream(t *testing.T) {
	var gotStream js.Value

	transformer := js.ValueOf(map[string]any{})
	transformer.Set("transform", js.FuncOf(func(this js.Value, args []js.Value) any {
		return this
	}))

	fake := js.ValueOf(map[string]any{})
	fake.Set("input", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotStream = args[0]
		return transformer
	}))

	binding := ImagesBindingFromJS(fake)
	tr, err := binding.Input(strings.NewReader("image bytes"), ImageInputOptions{})
	if err != nil {
		t.Fatalf("Input() failed: %v", err)
	}
	if tr == nil {
		t.Fatal("Input() returned a nil *ImageTransformer")
	}

	// A ReadableStream (as opposed to the io.Reader Go value or a plain
	// Uint8Array) exposes getReader.
	if gotStream.IsUndefined() || gotStream.Get("getReader").IsUndefined() {
		t.Errorf("Input() did not pass a ReadableStream to the JS call, got %v", gotStream)
	}

	tr2, err := tr.Transform(ImageTransform{Width: 100})
	if err != nil {
		t.Fatalf("Transform() failed: %v", err)
	}
	if !tr2.JSValue().Equal(tr.JSValue()) {
		t.Errorf("Transform() chain did not decode to the same underlying handle")
	}
}
