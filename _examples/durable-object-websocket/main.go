// Command durable-object-websocket is an example of hosting a Go type as a
// Cloudflare Durable Object class that accepts hibernatable WebSocket
// connections (exp/cloudflare/websocket's UpgradeHibernating +
// exp/cloudflare/durableobjects' AcceptHibernatingConn), broadcasting every
// message it receives to every other connection in the same Room.
package main

import (
	"context"
	"log"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/exp/cloudflare/durableobjects"
	"github.com/syumai/workers-go/exp/cloudflare/websocket"
)

// roomTag tags every WebSocket this Room accepts, so GetWebSockets(roomTag)
// can list them all when broadcasting a message.
const roomTag = "room"

// Room is a Durable Object: every client that connects to it joins the
// same broadcast group, for the lifetime of the (single, well-known) Room
// instance -- see NewRoom and handleIndex below, which always resolve to
// the same Durable Object ID ("global").
type Room struct {
	state *durableobjects.DurableObjectState
}

func NewRoom(state *durableobjects.DurableObjectState, env js.Value) (durableobjects.Object, error) {
	return &Room{state: state}, nil
}

// ServeHTTP handles the Room Durable Object's fetch() trigger: it upgrades
// the request to a hibernatable WebSocket and accepts it, then returns
// immediately -- there is no read/write loop to run here (unlike
// _examples/websocket-echo's plain, non-hibernating Conn), since incoming
// messages are delivered to WebSocketMessage below instead, possibly by a
// different wasm instance entirely once this one hibernates.
func (rm *Room) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.UpgradeHibernating(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := rm.state.AcceptHibernatingConn(conn, roomTag); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// WebSocketMessage implements durableobjects.WebSocketMessageHandler: it
// broadcasts every message to every WebSocket accepted under roomTag,
// including the sender (a simple chat room echoes your own messages back
// too).
func (rm *Room) WebSocketMessage(ctx context.Context, conn *websocket.HibernatingConn, mt websocket.MessageType, data []byte) error {
	peers, err := rm.state.GetWebSockets(roomTag)
	if err != nil {
		return err
	}
	for _, peer := range peers {
		if err := websocket.HibernatingConnFromJS(peer).WriteMessage(mt, data); err != nil {
			log.Println("durable-object-websocket: broadcast:", err)
		}
	}
	return nil
}

// WebSocketClose implements durableobjects.WebSocketCloseHandler, just to
// demonstrate that it's dispatched the same way WebSocketMessage is (only
// if implemented); a real chat room might announce departures here.
func (rm *Room) WebSocketClose(ctx context.Context, conn *websocket.HibernatingConn, code int, reason string, wasClean bool) error {
	log.Printf("durable-object-websocket: connection closed (code=%d reason=%q wasClean=%v)", code, reason, wasClean)
	return nil
}

func main() {
	// Register must run before workers.Serve -- see
	// exp/cloudflare/durableobjects' package doc comment.
	durableobjects.Register("Room", NewRoom)

	http.HandleFunc("/", handleIndex)
	workers.Serve(nil)
}

// handleIndex is this Worker's regular fetch handler: it forwards every
// request to the single Room instance named "global", the same way
// _examples/durable-object-go's handleIndex does for Counter -- except a
// WebSocket upgrade request needs cloudflare.DurableObjectStub.FetchWebSocket
// instead of Fetch, since Fetch reconstructs a *http.Response and has
// nowhere to carry the special WebSocket pairing a 101 response needs.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	ns, err := cloudflare.NewDurableObjectNamespace("ROOM")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := ns.IdFromName("global")
	stub, err := ns.Get(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := stub.FetchWebSocket(w, r); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
}
