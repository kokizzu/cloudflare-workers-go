//go:build js && wasm

package websocket

import (
	"context"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// Conn is the server side of an accepted WebSocket connection, wrapping the
// "server" end of a WebSocketPair. Obtain one via Upgrade.
type Conn struct {
	server js.Value

	msgCh chan wsMessage
	done  chan struct{}

	closeOnce sync.Once
	closeErr  error

	funcs []js.Func // message/close/error listeners, released on Close
}

type wsMessage struct {
	msgType MessageType
	data    []byte
}

// newConn wraps server (the "1" end of a WebSocketPair, already accepted)
// and registers its message/close/error listeners.
func newConn(server js.Value) *Conn {
	c := &Conn{
		server: server,
		msgCh:  make(chan wsMessage),
		done:   make(chan struct{}),
	}

	onMessage := js.FuncOf(func(this js.Value, args []js.Value) any {
		msg := decodeMessage(args[0])
		select {
		case c.msgCh <- msg:
		case <-c.done:
		}
		return js.Undefined()
	})
	onClose := js.FuncOf(func(this js.Value, args []js.Value) any {
		c.terminate(ErrClosed)
		return js.Undefined()
	})
	onError := js.FuncOf(func(this js.Value, args []js.Value) any {
		c.terminate(fmt.Errorf("websocket: connection closed after error"))
		return js.Undefined()
	})
	c.funcs = []js.Func{onMessage, onClose, onError}

	server.Call("addEventListener", "message", onMessage)
	server.Call("addEventListener", "close", onClose)
	server.Call("addEventListener", "error", onError)

	return c
}

// decodeMessage converts a WebSocket "message" event into a wsMessage: a JS
// string payload becomes TextMessage, anything else (an ArrayBuffer, since
// binaryType defaults to "arraybuffer" on the Workers runtime) becomes
// BinaryMessage.
func decodeMessage(event js.Value) wsMessage {
	data := event.Get("data")
	if data.Type() == js.TypeString {
		return wsMessage{msgType: TextMessage, data: []byte(data.String())}
	}
	// js.CopyBytesToGo (via jsrt.BytesFromJS) requires a Uint8Array view,
	// not a bare ArrayBuffer.
	view := js.Global().Get("Uint8Array").New(data)
	return wsMessage{msgType: BinaryMessage, data: jsrt.BytesFromJS(view)}
}

// terminate records err (if this is the first termination) and wakes up any
// blocked ReadMessage call. Safe to call more than once.
func (c *Conn) terminate(err error) {
	c.closeOnce.Do(func() {
		c.closeErr = err
		close(c.done)
	})
}

// ReadMessage blocks until a message arrives, the connection is closed, ctx
// is done, or an error event is observed. Once the connection is closed,
// ReadMessage returns ErrClosed (or, for an error event, a wrapping error)
// on this and every subsequent call.
func (c *Conn) ReadMessage(ctx context.Context) (MessageType, []byte, error) {
	select {
	case msg := <-c.msgCh:
		return msg.msgType, msg.data, nil
	case <-c.done:
		return 0, nil, c.closeErr
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// WriteMessage sends a single WebSocket message: TextMessage sends data as
// a JS string, BinaryMessage sends it as a Uint8Array.
func (c *Conn) WriteMessage(t MessageType, data []byte) error {
	switch t {
	case TextMessage:
		_, err := jsrt.Call(c.server, "send", string(data))
		return err
	case BinaryMessage:
		_, err := jsrt.Call(c.server, "send", jsrt.BytesToJS(data))
		return err
	default:
		return fmt.Errorf("websocket: WriteMessage: unknown message type %v", t)
	}
}

// Close closes the connection, calling the JS WebSocket close(code, reason)
// method (or close() with no arguments when code is 0) and releasing the
// event listeners registered by Upgrade. It is safe to call more than once;
// only the first call's code/reason are sent to the peer.
func (c *Conn) Close(code int, reason string) error {
	var err error
	c.closeOnce.Do(func() {
		c.closeErr = ErrClosed
		close(c.done)
		if code == 0 {
			_, err = jsrt.Call(c.server, "close")
		} else {
			_, err = jsrt.Call(c.server, "close", code, reason)
		}
	})
	for _, f := range c.funcs {
		f.Release()
	}
	return err
}
