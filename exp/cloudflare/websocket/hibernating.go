//go:build js && wasm

package websocket

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall/js"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// HibernatingConn wraps the server end of a WebSocket meant for the
// hibernatable WebSocket API
// (https://developers.cloudflare.com/durable-objects/best-practices/websockets/),
// obtained via UpgradeHibernating and then registered with
// exp/cloudflare/durableobjects.DurableObjectState.AcceptHibernatingConn.
//
// Unlike Conn, HibernatingConn has no ReadMessage: once accepted, incoming
// messages are delivered to the Durable Object's webSocketMessage()
// trigger (exp/cloudflare/durableobjects.WebSocketMessageHandler) — quite
// possibly to a brand new wasm instance, since hibernation means the
// instance that called UpgradeHibernating need not still be running when a
// message arrives — rather than read from a channel on this value.
type HibernatingConn struct {
	v js.Value
}

// HibernatingConnFromJS wraps a JS WebSocket value — typically the ws
// argument a durableobjects.WebSocketMessageHandler/WebSocketCloseHandler/
// WebSocketErrorHandler method receives — as a *HibernatingConn.
func HibernatingConnFromJS(v js.Value) *HibernatingConn { return &HibernatingConn{v: v} }

// JSValue returns the underlying JS WebSocket value.
func (c *HibernatingConn) JSValue() js.Value { return c.v }

// WriteMessage sends a single WebSocket message: TextMessage sends data as
// a JS string, BinaryMessage sends it as a Uint8Array.
func (c *HibernatingConn) WriteMessage(t MessageType, data []byte) error {
	switch t {
	case TextMessage:
		_, err := jsrt.Call(c.v, "send", string(data))
		return err
	case BinaryMessage:
		_, err := jsrt.Call(c.v, "send", jsrt.BytesToJS(data))
		return err
	default:
		return fmt.Errorf("websocket: HibernatingConn.WriteMessage: unknown message type %v", t)
	}
}

// Close closes the connection, calling the JS WebSocket close(code, reason)
// method (or close() with no arguments when code is 0).
func (c *HibernatingConn) Close(code int, reason string) error {
	if code == 0 {
		_, err := jsrt.Call(c.v, "close")
		return err
	}
	_, err := jsrt.Call(c.v, "close", code, reason)
	return err
}

// UpgradeHibernating upgrades an incoming HTTP request to a WebSocket
// connection meant for the hibernatable WebSocket API.
//
// Unlike Upgrade, it does NOT call accept() on the server end: the caller
// is expected to hand the returned *HibernatingConn to
// exp/cloudflare/durableobjects.DurableObjectState.AcceptHibernatingConn,
// which calls acceptWebSocket() (not accept()) to register it for the
// owning Durable Object's webSocketMessage/webSocketClose/webSocketError
// triggers instead of an in-process event-listener loop — so this must
// only be used from within a Durable Object's fetch() trigger (see
// exp/cloudflare/durableobjects). Use Upgrade instead for a regular
// (non-hibernating, non-Durable-Object) WebSocket handler.
//
// See the package doc comment on Upgrade for the constraint that the
// handler must return promptly without blocking on the connection.
func UpgradeHibernating(w http.ResponseWriter, r *http.Request) (*HibernatingConn, error) {
	if got := r.Header.Get("Upgrade"); !strings.EqualFold(got, "websocket") {
		return nil, fmt.Errorf("websocket: UpgradeHibernating: expected request header Upgrade: websocket, got %q", got)
	}
	setter, ok := w.(webSocketSetter)
	if !ok {
		return nil, fmt.Errorf("websocket: UpgradeHibernating: %T does not support attaching a WebSocket to its response", w)
	}
	pairClass := js.Global().Get("WebSocketPair")
	if pairClass.IsUndefined() {
		return nil, errors.New("websocket: UpgradeHibernating: WebSocketPair is not available in this runtime")
	}
	pair := pairClass.New()
	client := pair.Get("0")
	server := pair.Get("1")

	setter.SetWebSocket(client)
	return &HibernatingConn{v: server}, nil
}
