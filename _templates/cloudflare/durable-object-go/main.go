package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/exp/cloudflare/durableobjects"
)

// countKey is the DurableObjectStorage key the request count is persisted
// under, so it survives the Durable Object being evicted and reconstructed.
const countKey = "count"

// Counter is a Durable Object: one instance is created per Durable Object
// ID. See idFromName("global") in handleIndex below, which always resolves
// to the same ID -- and therefore the same instance -- for this whole
// Worker.
type Counter struct {
	state *durableobjects.DurableObjectState
}

// NewCounter is the durableobjects.Constructor registered for the "Counter"
// class in main below.
func NewCounter(state *durableobjects.DurableObjectState, env js.Value) (durableobjects.Object, error) {
	return &Counter{state: state}, nil
}

// ServeHTTP handles the Counter Durable Object's fetch() trigger: it
// increments the persisted count and returns its new value.
func (c *Counter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	storage := c.state.Storage()
	var count int
	if _, err := storage.GetJSON(countKey, &count); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	count++
	if err := storage.PutJSON(countKey, count); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "count=%d\n", count)
}

func main() {
	// Register must run before workers.Serve: it tells
	// exp/cloudflare/durableobjects how to construct a Counter the first
	// time this Worker instance receives a fetch trigger for the "Counter"
	// Durable Object class -- see package.json's -durable-objects=Counter
	// flag and wrangler.jsonc's durable_objects.bindings class_name, both
	// of which must name the class the same way.
	durableobjects.Register("Counter", NewCounter)

	http.HandleFunc("/", handleIndex)
	workers.Serve(nil)
}

// handleIndex is this Worker's regular fetch handler (unrelated to the
// Durable Object's own fetch() trigger above): it forwards every request to
// the single Counter instance named "global", using
// cloudflare.DurableObjectNamespace/-Stub, the way any Worker talks to a
// Durable Object it doesn't happen to be hosting itself.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	ns, err := cloudflare.NewDurableObjectNamespace("COUNTER")
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
	res, err := stub.Fetch(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer res.Body.Close()
	for key, values := range res.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(res.StatusCode)
	if _, err := io.Copy(w, res.Body); err != nil {
		log.Printf("durable-object-go: failed to copy response body: %v", err)
	}
}
