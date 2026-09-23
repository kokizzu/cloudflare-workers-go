//go:build js && wasm

package jshttp

import (
	"bytes"
	"net/http"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jsutil"
)

// newFakeJSRequest builds a JavaScript standard Request directly (bypassing
// ToJSRequest, since request_test.go exercises the opposite direction,
// ToRequest) with full control over headers that ToJSRequest itself would
// never set (Host, Content-Length, Transfer-Encoding, Cf-Connecting-Ip).
func newFakeJSRequest(t *testing.T, method, url string, header http.Header, body []byte) js.Value {
	t.Helper()
	h := jsutil.HeadersClass.New()
	for name, values := range header {
		for _, v := range values {
			h.Call("append", name, v)
		}
	}
	init := jsutil.NewObject()
	init.Set("method", method)
	init.Set("headers", h)
	if body != nil {
		ua := jsutil.NewUint8Array(len(body))
		if len(body) > 0 {
			js.CopyBytesToJS(ua, body)
		}
		init.Set("body", ua)
	}
	return jsutil.RequestClass.New(url, init)
}

func TestToRequest(t *testing.T) {
	t.Run("method_url_host", func(t *testing.T) {
		header := http.Header{"Host": {"example.com"}}
		jsReq := newFakeJSRequest(t, http.MethodPost, "https://example.com/path?q=1", header, nil)

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		if req.Method != http.MethodPost {
			t.Errorf("Method = %q, want %q", req.Method, http.MethodPost)
		}
		if got := req.URL.String(); got != "https://example.com/path?q=1" {
			t.Errorf("URL = %q, want %q", got, "https://example.com/path?q=1")
		}
		if req.Host != "example.com" {
			t.Errorf("Host = %q, want %q", req.Host, "example.com")
		}
	})

	t.Run("remote_addr_from_cf_connecting_ip", func(t *testing.T) {
		header := http.Header{"Cf-Connecting-Ip": {"1.2.3.4"}}
		jsReq := newFakeJSRequest(t, http.MethodGet, "https://example.com/", header, nil)

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		if req.RemoteAddr != "1.2.3.4" {
			t.Errorf("RemoteAddr = %q, want %q", req.RemoteAddr, "1.2.3.4")
		}
	})

	t.Run("content_length_present", func(t *testing.T) {
		body := []byte("hello")
		header := http.Header{"Content-Length": {"5"}}
		jsReq := newFakeJSRequest(t, http.MethodPost, "https://example.com/", header, body)

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		if req.ContentLength != 5 {
			t.Errorf("ContentLength = %d, want 5", req.ContentLength)
		}
	})

	t.Run("content_length_missing_with_body", func(t *testing.T) {
		body := []byte("hello")
		jsReq := newFakeJSRequest(t, http.MethodPost, "https://example.com/", http.Header{}, body)

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		if req.ContentLength != -1 {
			t.Errorf("ContentLength = %d, want -1", req.ContentLength)
		}
	})

	t.Run("content_length_missing_without_body", func(t *testing.T) {
		jsReq := newFakeJSRequest(t, http.MethodGet, "https://example.com/", http.Header{}, nil)

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		if req.ContentLength != 0 {
			t.Errorf("ContentLength = %d, want 0", req.ContentLength)
		}
	})

	t.Run("transfer_encoding_split", func(t *testing.T) {
		header := http.Header{"Transfer-Encoding": {"chunked, gzip"}}
		jsReq := newFakeJSRequest(t, http.MethodPost, "https://example.com/", header, []byte("x"))

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		want := []string{"chunked", "gzip"}
		if len(req.TransferEncoding) != len(want) {
			t.Fatalf("TransferEncoding = %q, want %q", req.TransferEncoding, want)
		}
		for i := range want {
			if req.TransferEncoding[i] != want[i] {
				t.Errorf("TransferEncoding[%d] = %q, want %q", i, req.TransferEncoding[i], want[i])
			}
		}
	})

	t.Run("body_null", func(t *testing.T) {
		jsReq := newFakeJSRequest(t, http.MethodGet, "https://example.com/", http.Header{}, nil)

		req, err := ToRequest(jsReq)
		if err != nil {
			t.Fatalf("ToRequest() error = %v, want nil", err)
		}
		if req.Body != http.NoBody {
			t.Errorf("Body = %v, want http.NoBody", req.Body)
		}
	})
}

func TestToJSRequest(t *testing.T) {
	t.Run("post_with_body", func(t *testing.T) {
		body := []byte("payload")
		req, err := http.NewRequest(http.MethodPost, "https://example.com/path", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("http.NewRequest() error = %v, want nil", err)
		}
		req.Header.Set("X-Test", "1")

		jsReq := ToJSRequest(req)
		if got := jsReq.Get("method").String(); got != http.MethodPost {
			t.Errorf("method = %q, want %q", got, http.MethodPost)
		}
		if got := jsReq.Get("url").String(); got != "https://example.com/path" {
			t.Errorf("url = %q, want %q", got, "https://example.com/path")
		}
		if got := jsReq.Get("headers").Call("get", "X-Test").String(); got != "1" {
			t.Errorf("headers.get(X-Test) = %q, want %q", got, "1")
		}

		// The body is read via Request.text() (native JS stream
		// consumption): reading it through this package's own
		// ConvertReadableStreamToReadCloser is covered separately by
		// TestToJSResponse_ReadAll.
		v, err := jsutil.AwaitPromise(jsReq.Call("text"))
		if err != nil {
			t.Fatalf("jsReq.text() error = %v, want nil", err)
		}
		if got := v.String(); got != string(body) {
			t.Errorf("body = %q, want %q", got, body)
		}
	})

	t.Run("get_has_no_body", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://example.com/path", nil)
		if err != nil {
			t.Fatalf("http.NewRequest() error = %v, want nil", err)
		}

		jsReq := ToJSRequest(req)
		if body := jsReq.Get("body"); !body.IsNull() {
			t.Errorf("body = %v, want null", body)
		}
	})
}

func TestToBody_null(t *testing.T) {
	if got := ToBody(jsutil.Null); got != http.NoBody {
		t.Errorf("ToBody(null) = %v, want http.NoBody", got)
	}
}
