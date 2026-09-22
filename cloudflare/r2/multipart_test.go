//go:build js && wasm

package r2

import (
	"strings"
	"syscall/js"
	"testing"

	r2js "github.com/syumai/workers-go/exp/cloudflare/r2"
)

func resolve(v any) js.Value {
	return js.Global().Get("Promise").Call("resolve", v)
}

// TestMultipartUpload_UploadPart verifies UploadPart passes the requested
// partNumber through and converts its io.Reader argument into a JS
// ReadableStream (the parameter-position mapping cfgen gives
// ReadableStream<...>, per tmp/06-codegen-spec.md 3.1.1), and that the
// decoded *UploadedPart carries back the fake's partNumber/etag.
func TestMultipartUpload_UploadPart(t *testing.T) {
	var gotPartNumber int
	var gotValue js.Value

	fake := js.ValueOf(map[string]any{
		"key":      "my-key",
		"uploadId": "upload-1",
	})
	fake.Set("uploadPart", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotPartNumber = args[0].Int()
		gotValue = args[1]
		return resolve(map[string]any{
			"partNumber": args[0],
			"etag":       "etag-part-1",
		})
	}))

	upload := toMultipartUpload(r2js.R2MultipartUploadFromJS(fake))
	if upload.Key != "my-key" || upload.UploadID != "upload-1" {
		t.Fatalf("toMultipartUpload() = %+v, want Key=my-key UploadID=upload-1", upload)
	}

	part, err := upload.UploadPart(3, strings.NewReader("part body"))
	if err != nil {
		t.Fatalf("UploadPart() failed: %v", err)
	}
	if gotPartNumber != 3 {
		t.Errorf("uploadPart received partNumber = %d, want 3", gotPartNumber)
	}
	// A ReadableStream (as opposed to the io.Reader Go value or a plain
	// Uint8Array) exposes getReader.
	if gotValue.IsUndefined() || gotValue.Get("getReader").IsUndefined() {
		t.Errorf("UploadPart() did not pass a ReadableStream to the JS call, got %v", gotValue)
	}
	if part.PartNumber != 3 {
		t.Errorf("part.PartNumber = %d, want 3", part.PartNumber)
	}
	if part.ETag != "etag-part-1" {
		t.Errorf("part.ETag = %q, want %q", part.ETag, "etag-part-1")
	}
}
