// Command rpc-go is an example of hosting a Go type's methods as a
// Cloudflare Workers RPC WorkerEntrypoint (exp/cloudflare/rpc), plus a
// regular HTTP handler that calls into that same entrypoint over a Service
// binding to itself (env.SELF, per wrangler.toml's [[services]] entrypoint
// = "MyService").
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/rpc"
)

// addMethod implements MyService.add(a, b): a rpc.MethodJSON wrapping a
// typed Go function. RPC arguments arrive positionally, so args[0]/args[1]
// are JSON-decoded individually instead of as one combined value.
var addMethod = rpc.MethodJSON(func(ctx context.Context, args []json.RawMessage) (int, error) {
	var a, b int
	if len(args) > 0 {
		if err := json.Unmarshal(args[0], &a); err != nil {
			return 0, err
		}
	}
	if len(args) > 1 {
		if err := json.Unmarshal(args[1], &b); err != nil {
			return 0, err
		}
	}
	return a + b, nil
})

// greetMethod implements MyService.greet(name).
var greetMethod = rpc.MethodJSON(func(ctx context.Context, args []json.RawMessage) (string, error) {
	var name string
	if len(args) > 0 {
		if err := json.Unmarshal(args[0], &name); err != nil {
			return "", err
		}
	}
	return "hello, " + name, nil
})

func main() {
	// Register/RegisterFetch must run before workers.Serve -- they tell
	// exp/cloudflare/rpc how to run "MyService"'s RPC methods and its own
	// fetch() trigger, matching cmd/workers-assets-gen's
	// -entrypoints=MyService:add,greet flag (Makefile) and
	// wrangler.toml's [[services]] entrypoint, both of which name the
	// class the same way.
	rpc.Register("MyService", map[string]rpc.Method{
		"add":   addMethod,
		"greet": greetMethod,
	})
	rpc.RegisterFetch("MyService", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("MyService's own fetch() trigger, path=" + r.URL.Path))
	}))

	// The Worker's regular fetch handler (unrelated to MyService's RPC
	// methods/fetch() above): it calls into MyService's add/greet through
	// the SELF Service binding, the same way any other Worker would call
	// into a Service binding pointing at this Worker's "MyService"
	// entrypoint.
	http.HandleFunc("/", handleIndex)
	workers.Serve(nil)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	stub, err := rpc.NewStub("SELF")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var sum int
	if err := stub.CallJSON("add", &sum, 1, 2); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var greeting string
	if err := stub.CallJSON("greet", &greeting, "go"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"sum": sum, "greeting": greeting}); err != nil {
		log.Printf("rpc-go: failed to encode response: %v", err)
	}
}
