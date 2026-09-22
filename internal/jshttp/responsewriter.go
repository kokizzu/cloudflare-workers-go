package jshttp

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

type ResponseWriter struct {
	HeaderValue http.Header
	StatusCode  int
	Reader      io.ReadCloser
	Writer      *io.PipeWriter
	ReadyCh     chan struct{}
	Once        sync.Once
	RawJSBody   *js.Value

	// webSocket, when set via SetWebSocket, is attached to the Response
	// built by ToJSResponse as ResponseInit.webSocket, forcing status 101
	// and a null body. It is the zero js.Value (IsUndefined() == true)
	// until SetWebSocket is called.
	webSocket js.Value
}

var (
	_ http.ResponseWriter    = (*ResponseWriter)(nil)
	_ jsutil.RawJSBodyWriter = (*ResponseWriter)(nil)
	_ http.Flusher           = (*ResponseWriter)(nil)
)

// Ready indicates that ResponseWriter is ready to be converted to Response.
func (w *ResponseWriter) Ready() {
	w.Once.Do(func() {
		close(w.ReadyCh)
	})
}

func (w *ResponseWriter) Write(data []byte) (n int, err error) {
	w.Ready()
	return w.Writer.Write(data)
}

func (w *ResponseWriter) Header() http.Header {
	return w.HeaderValue
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	w.StatusCode = statusCode
}

func (w *ResponseWriter) WriteRawJSBody(body js.Value) {
	w.RawJSBody = &body
}

// SetWebSocket attaches ws (the client end of a WebSocketPair) to the
// Response ToJSResponse builds: the response is upgraded to status 101
// Switching Protocols with ResponseInit.webSocket set to ws and no body,
// regardless of what WriteHeader/Write were called with.
//
// Callers reach this indirectly through exp/cloudflare/websocket.Upgrade,
// which type-asserts the http.ResponseWriter it is given against an
// interface{ SetWebSocket(js.Value) } rather than importing this package's
// concrete type directly.
func (w *ResponseWriter) SetWebSocket(ws js.Value) {
	w.webSocket = ws
}

// Flush is a no-op implementation of http.Flusher.
//
// * PipeWriter does not have buffer, and JS-side Response does not have flush method.
// * But some libraries like `mcp-go` requires this method.
// * So implement this method as a workaround.
func (w *ResponseWriter) Flush() {
	// no-op
}

// ToJSResponse converts *ResponseWriter to JavaScript sides Response.
//   - Response: https://developer.mozilla.org/docs/Web/API/Response
func (w *ResponseWriter) ToJSResponse() js.Value {
	contentLength, _ := strconv.ParseInt(w.HeaderValue.Get("Content-Length"), 10, 64)
	var webSocket *js.Value
	if !w.webSocket.IsUndefined() {
		webSocket = &w.webSocket
	}
	return newJSResponse(w.StatusCode, w.HeaderValue, contentLength, w.Reader, w.RawJSBody, webSocket)
}
