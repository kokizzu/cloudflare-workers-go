// Command durable-object-go is an example of hosting a Go type as a
// Cloudflare Durable Object class (exp/cloudflare/durableobjects), fronted
// by a regular HTTP handler that forwards every request to a single,
// well-known Counter instance.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/exp/cloudflare/durableobjects"
)

// countKey is the DurableObjectStorage key the request count is persisted
// under, so it survives the Durable Object being evicted and reconstructed.
const countKey = "count"

// Counter is a Durable Object: one instance is created per Durable Object
// ID (see NewCounter's log line, and idFromName("global") below, which
// always resolves to the same ID -- and therefore the same instance -- for
// this whole Worker).
type Counter struct {
	state *durableobjects.DurableObjectState
}

// NewCounter is the durableobjects.Constructor registered for the "Counter"
// class in main below. exp/cloudflare/durableobjects guarantees this runs
// at most once per Durable Object instance (its wasm instance stays alive
// across every subsequent fetch/alarm trigger), so the log line below
// prints once per instance, not once per request -- run `make dev` and curl
// the Worker twice to see that for yourself.
func NewCounter(state *durableobjects.DurableObjectState, env js.Value) (durableobjects.Object, error) {
	log.Println("Counter: constructing new instance")
	return &Counter{state: state}, nil
}

// ServeHTTP handles the Counter Durable Object's fetch() trigger.
//   - GET /       increments the persisted count and returns its new value.
//   - GET /alarm  schedules an Alarm 5 seconds from now (see Alarm below).
func (c *Counter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	storage := c.state.Storage()
	switch r.URL.Path {
	case "/alarm":
		when := time.Now().Add(5 * time.Second)
		if err := storage.SetAlarm(when, durableobjects.DurableObjectSetAlarmOptions{}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "alarm scheduled for %s\n", when.Format(time.RFC3339))
	default:
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
}

// Alarm implements durableobjects.AlarmHandler, demonstrating that a
// Durable Object's alarm() trigger dispatches to this Object the same way
// fetch() does -- see GET /alarm above, which schedules one.
func (c *Counter) Alarm(ctx context.Context, info *durableobjects.AlarmInvocationInfo) error {
	log.Println("Counter: alarm fired")
	return nil
}

func main() {
	// Register must run before workers.Serve: it tells
	// exp/cloudflare/durableobjects how to construct a Counter the first
	// time this Worker instance receives a trigger (fetch/alarm/webSocket*)
	// for the "Counter" Durable Object class -- see
	// cmd/workers-assets-gen's -durable-objects=Counter flag (Makefile)
	// and wrangler.toml's [[durable_objects.bindings]] class_name, both of
	// which must name the class the same way.
	durableobjects.Register("Counter", NewCounter)

	http.HandleFunc("/", handleIndex)
	workers.Serve(nil)
}

// handleIndex is this Worker's regular fetch handler (unrelated to the
// Durable Object's own fetch() trigger above): it forwards every request to
// the single Counter instance named "global", using the hand-written
// cloudflare.DurableObjectNamespace/-Stub (cloudflare/dostub.go) the way any
// Worker talks to a Durable Object it doesn't happen to be hosting itself.
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
