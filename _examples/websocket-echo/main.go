package main

import (
	"context"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/websocket"
)

func main() {
	http.HandleFunc("/", handleWebSocket)
	workers.Serve(nil)
}

func handleWebSocket(w http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Upgrade(w, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Upgrade only sets up the 101 response; it is sent once this handler
	// returns. So the echo loop below must run in its own goroutine, and
	// this handler must return immediately without touching conn.
	go echoLoop(conn)
}

func echoLoop(conn *websocket.Conn) {
	defer conn.Close(0, "")
	ctx := context.Background()
	for {
		msgType, data, err := conn.ReadMessage(ctx)
		if err != nil {
			log.Println("websocket-echo: ReadMessage:", err)
			return
		}
		if err := conn.WriteMessage(msgType, data); err != nil {
			log.Println("websocket-echo: WriteMessage:", err)
			return
		}
	}
}
