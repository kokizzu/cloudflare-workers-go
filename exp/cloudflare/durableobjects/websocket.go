//go:build js && wasm

package durableobjects

import (
	"github.com/syumai/workers-go/exp/cloudflare/websocket"
)

// AcceptHibernatingConn registers c — typically obtained from
// exp/cloudflare/websocket.UpgradeHibernating, called from within this
// Durable Object's fetch() trigger — for the hibernatable WebSocket API
// (DurableObjectState.acceptWebSocket). Once this returns, the owning
// Durable Object's webSocketMessage/webSocketClose/webSocketError triggers
// (see WebSocketMessageHandler/WebSocketCloseHandler/WebSocketErrorHandler)
// deliver events for c — even across the object being evicted and
// hibernated between messages, unlike exp/cloudflare/websocket.Conn's
// in-memory event-listener loop, which only lives as long as the goroutine
// that called Upgrade.
//
// tags (at most 10, each at most 256 bytes, per the runtime's own limits)
// let GetTags/GetWebSockets look c back up later, e.g. from a different
// trigger's Object instance after hibernation. See
// https://developers.cloudflare.com/durable-objects/api/state/#getwebsockets.
func (x *DurableObjectState) AcceptHibernatingConn(c *websocket.HibernatingConn, tags ...string) error {
	return x.AcceptWebSocket(c.JSValue(), tags)
}
