//go:build js && wasm

package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// withLenientResponseClass swaps jsutil.ResponseClass for a fake constructor
// that just records its (body, init) arguments as JS properties on a plain
// object, restoring the original on cleanup. Node's real Response
// constructor (used by the wasm test harness) rejects status 101 outright,
// even though Cloudflare Workers' own Response class special-cases 101 for
// the WebSocket upgrade protocol.
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

// fakeWebSocketPair installs a fake global WebSocketPair constructor that
// returns a {0: client, 1: server} pair. server supports accept/send/close
// (recorded into the returned *fakeServer) and addEventListener (listeners
// are recorded, and can be invoked from tests to simulate JS-side events).
// It returns the fake client/server values plus a cleanup func that restores
// the previous WebSocketPair global.
func fakeWebSocketPair(t *testing.T) (client, server js.Value, fs *fakeServer) {
	t.Helper()
	fs = &fakeServer{listeners: map[string][]js.Value{}}

	client = js.ValueOf(map[string]any{})
	server = js.ValueOf(map[string]any{})

	server.Set("accept", js.FuncOf(func(this js.Value, args []js.Value) any {
		fs.mu.Lock()
		fs.accepted = true
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("send", js.FuncOf(func(this js.Value, args []js.Value) any {
		fs.mu.Lock()
		fs.sent = append(fs.sent, args[0])
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("close", js.FuncOf(func(this js.Value, args []js.Value) any {
		fs.mu.Lock()
		fs.closed = true
		if len(args) > 0 {
			fs.closeCode = args[0].Int()
		}
		if len(args) > 1 {
			fs.closeReason = args[1].String()
		}
		fs.mu.Unlock()
		return js.Undefined()
	}))
	server.Set("addEventListener", js.FuncOf(func(this js.Value, args []js.Value) any {
		name := args[0].String()
		fs.mu.Lock()
		fs.listeners[name] = append(fs.listeners[name], args[1])
		fs.mu.Unlock()
		return js.Undefined()
	}))

	pair := js.ValueOf(map[string]any{})
	pair.Set("0", client)
	pair.Set("1", server)

	prevWebSocketPair := js.Global().Get("WebSocketPair")
	ctor := js.FuncOf(func(this js.Value, args []js.Value) any {
		return pair
	})
	js.Global().Set("WebSocketPair", ctor)
	t.Cleanup(func() {
		ctor.Release()
		if prevWebSocketPair.IsUndefined() {
			js.Global().Delete("WebSocketPair")
		} else {
			js.Global().Set("WebSocketPair", prevWebSocketPair)
		}
	})

	return client, server, fs
}

type fakeServer struct {
	mu          sync.Mutex
	accepted    bool
	sent        []js.Value
	closed      bool
	closeCode   int
	closeReason string
	listeners   map[string][]js.Value
}

func (fs *fakeServer) dispatch(name string, event js.Value) {
	fs.mu.Lock()
	ls := append([]js.Value(nil), fs.listeners[name]...)
	fs.mu.Unlock()
	for _, l := range ls {
		l.Invoke(event)
	}
}

func newUpgradeRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ws", nil)
	r.Header.Set("Upgrade", "websocket")
	return r
}

func newResponseWriter() *jshttp.ResponseWriter {
	return &jshttp.ResponseWriter{
		HeaderValue: http.Header{},
		StatusCode:  http.StatusOK,
	}
}

func TestUpgrade_MissingHeader(t *testing.T) {
	fakeWebSocketPair(t)
	w := newResponseWriter()
	r := httptest.NewRequest(http.MethodGet, "/ws", nil) // no Upgrade header
	if _, err := Upgrade(w, r); err == nil {
		t.Fatal("Upgrade() with no Upgrade header succeeded, want error")
	}
}

func TestUpgrade_SetsWebSocketOnResponse(t *testing.T) {
	withLenientResponseClass(t)
	client, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	fs.mu.Lock()
	accepted := fs.accepted
	fs.mu.Unlock()
	if !accepted {
		t.Error("accept() was not called on the server end")
	}

	resp := w.ToJSResponse()
	if got := resp.Get("status").Int(); got != http.StatusSwitchingProtocols {
		t.Errorf("response status = %d, want %d", got, http.StatusSwitchingProtocols)
	}
	if got := resp.Get("webSocket"); !got.Equal(client) {
		t.Errorf("response webSocket = %v, want the WebSocketPair client end", got)
	}
}

func TestConn_WriteMessage(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	if err := conn.WriteMessage(TextMessage, []byte("hello")); err != nil {
		t.Fatalf("WriteMessage(TextMessage) failed: %v", err)
	}
	if err := conn.WriteMessage(BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatalf("WriteMessage(BinaryMessage) failed: %v", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.sent) != 2 {
		t.Fatalf("server.send() called %d times, want 2", len(fs.sent))
	}
	if got := fs.sent[0].String(); got != "hello" {
		t.Errorf("first send() arg = %q, want %q", got, "hello")
	}
	if fs.sent[1].Get("constructor").Get("name").String() != "Uint8Array" {
		t.Errorf("second send() arg is not a Uint8Array: %v", fs.sent[1])
	}
}

func TestConn_ReadMessage_TextAndBinary(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	go fs.dispatch("message", js.ValueOf(map[string]any{"data": "hi there"}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mt, data, err := conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage() failed: %v", err)
	}
	if mt != TextMessage {
		t.Errorf("message type = %v, want TextMessage", mt)
	}
	if string(data) != "hi there" {
		t.Errorf("message data = %q, want %q", data, "hi there")
	}

	buf := js.Global().Get("Uint8Array").New(3)
	buf.SetIndex(0, 9)
	buf.SetIndex(1, 8)
	buf.SetIndex(2, 7)
	arrayBuffer := buf.Get("buffer")
	go fs.dispatch("message", js.ValueOf(map[string]any{"data": arrayBuffer}))

	mt, data, err = conn.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("ReadMessage() (binary) failed: %v", err)
	}
	if mt != BinaryMessage {
		t.Errorf("message type = %v, want BinaryMessage", mt)
	}
	if len(data) != 3 || data[0] != 9 || data[1] != 8 || data[2] != 7 {
		t.Errorf("message data = %v, want [9 8 7]", data)
	}
}

func TestConn_ReadMessage_Close(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	defer conn.Close(0, "")

	go fs.dispatch("close", js.ValueOf(map[string]any{}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.ReadMessage(ctx); err != ErrClosed {
		t.Fatalf("ReadMessage() after close event returned err = %v, want ErrClosed", err)
	}
	// Sticky: a second call also returns ErrClosed immediately.
	if _, _, err := conn.ReadMessage(ctx); err != ErrClosed {
		t.Fatalf("second ReadMessage() returned err = %v, want ErrClosed", err)
	}
}

func TestConn_Close(t *testing.T) {
	_, _, fs := fakeWebSocketPair(t)
	w := newResponseWriter()
	conn, err := Upgrade(w, newUpgradeRequest())
	if err != nil {
		t.Fatalf("Upgrade() failed: %v", err)
	}
	if err := conn.Close(1000, "bye"); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if !fs.closed {
		t.Error("close() was not called on the server end")
	}
	if fs.closeCode != 1000 || fs.closeReason != "bye" {
		t.Errorf("close() called with (%d, %q), want (1000, \"bye\")", fs.closeCode, fs.closeReason)
	}
}
