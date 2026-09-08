package jshttp

import (
	"context"
	"io"
	"net/http"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/internal/runtimecontext"
)

// bodyCloser wraps the io.Pipe reader handed to the JS-side Response body so
// that, once the JS runtime has fully consumed/closed it, onClosed (if
// non-nil) runs exactly once. This is how ServeRequest's caller learns that
// the Response body has been read to completion (or abandoned) by the JS
// side, since the handler's ServeHTTP goroutine may finish writing well
// before that happens for a streamed response.
type bodyCloser struct {
	io.ReadCloser
	onClosed func()
	once     sync.Once
}

func (c *bodyCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.onClosed != nil {
		c.once.Do(c.onClosed)
	}
	return err
}

// ServeRequest converts reqObj (a JS Request) to an *http.Request (via
// ToRequest), attaches reqObj to its context (via runtimecontext.New, so
// e.g. cloudflare/fetch.FromRequest and exp/cloudflare/cf.FromRequest can
// recover it later), runs handler against it on a new goroutine, and
// converts the result back to a JS Response (via ResponseWriter.ToJSResponse).
//
// If onBodyClosed is non-nil, it is called exactly once when the returned
// Response's body is closed (e.g. once the JS runtime has fully read a
// streamed response, or the request is otherwise torn down) — this is how a
// caller can be notified that reqObj's connection/instance is done with the
// response, without having to poll. Passing nil (as durableobjects/host.go
// does for a Durable Object's fetch handler, whose Go instance stays alive
// across multiple triggers rather than exiting when one response is done)
// simply skips that notification.
func ServeRequest(handler http.Handler, reqObj js.Value, onBodyClosed func()) (js.Value, error) {
	req, err := ToRequest(reqObj)
	if err != nil {
		return js.Value{}, err
	}
	ctx := runtimecontext.New(context.Background(), reqObj)
	req = req.WithContext(ctx)

	reader, writer := io.Pipe()
	w := &ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
		Reader:      &bodyCloser{ReadCloser: reader, onClosed: onBodyClosed},
		Writer:      writer,
		ReadyCh:     make(chan struct{}),
	}
	go func() {
		defer w.Ready()
		defer writer.Close()
		handler.ServeHTTP(w, req)
	}()
	<-w.ReadyCh
	return w.ToJSResponse(), nil
}
