// Command workflow-go is an example of hosting a Go type's run() as a
// Cloudflare Workflow (exp/cloudflare/workflows), plus a regular HTTP
// handler that creates and inspects instances of it through the
// client-side L1 (workflows.Workflow / workflows.WorkflowInstance).
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/workflows"
)

// step1Result is step "generate-message"'s JSON result.
type step1Result struct {
	Message string `json:"message"`
}

// step2Result is step "combine-message"'s JSON result: it reuses step 1's
// result, demonstrating that a step's return value is available (from Go
// memory, or replayed from the saved value) to later steps in the same
// run().
type step2Result struct {
	Combined string `json:"combined"`
}

// runMyWorkflow is the workflows.Runner registered for the "MyWorkflow"
// class in main below.
func runMyWorkflow(ctx context.Context, event *workflows.Event, step *workflows.Step) (js.Value, error) {
	log.Printf("MyWorkflow: run() started, instanceId=%s", event.InstanceID)

	step1, err := workflows.DoJSON(step, "generate-message", func(ctx context.Context) (step1Result, error) {
		log.Println("MyWorkflow: running step 1")
		return step1Result{Message: "hello from step 1"}, nil
	})
	if err != nil {
		return js.Value{}, err
	}

	if err := step.Sleep("wait-a-bit", 1*time.Second); err != nil {
		return js.Value{}, err
	}

	step2, err := workflows.DoJSON(step, "combine-message", func(ctx context.Context) (step2Result, error) {
		log.Println("MyWorkflow: running step 2")
		return step2Result{Combined: step1.Message + " + step 2"}, nil
	})
	if err != nil {
		return js.Value{}, err
	}

	return workflows.ResultJSON(map[string]any{
		"step1": step1,
		"step2": step2,
	})
}

func main() {
	// Register must run before workers.Serve: it tells
	// exp/cloudflare/workflows how to run "MyWorkflow"'s run(event, step)
	// -- see cmd/workers-assets-gen's -workflows=MyWorkflow flag (Makefile)
	// and wrangler.toml's [[workflows]] class_name, both of which must name
	// the class the same way.
	workflows.Register("MyWorkflow", runMyWorkflow)

	http.HandleFunc("/", handleIndex)
	workers.Serve(nil)
}

// handleIndex is this Worker's regular fetch handler (unrelated to the
// Workflow's own run() trigger above):
//   - POST / creates a new "MyWorkflow" instance (via the client-side L1's
//     Workflow.Create) and returns its id.
//   - GET /?id=<instanceId> returns that instance's current status (via
//     WorkflowInstance.Status), including run()'s result once it completes.
func handleIndex(w http.ResponseWriter, r *http.Request) {
	wf, err := workflows.NewWorkflow("MY_WORKFLOW")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodPost:
		instance, err := wf.Create(workflows.WorkflowInstanceCreateOptions{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]string{"id": instance.ID()}); err != nil {
			log.Printf("workflow-go: failed to encode response: %v", err)
		}
	default:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing \"id\" query parameter", http.StatusBadRequest)
			return
		}
		instance, err := wf.Get(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		status, err := instance.Status()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(statusResponse(status)); err != nil {
			log.Printf("workflow-go: failed to encode response: %v", err)
		}
	}
}

// statusResponse turns an InstanceStatus into a plain JSON-encodable value.
// InstanceStatus.Output is a raw, structured-clonable js.Value (run()'s
// resolved value once the instance completes) -- not something
// encoding/json can marshal on its own -- so it's re-encoded through the JS
// side's JSON.stringify first, the same round trip
// exp/cloudflare/durableobjects.DurableObjectStorage.GetJSON uses.
func statusResponse(status workflows.InstanceStatus) map[string]any {
	resp := map[string]any{"status": status.Status}
	if status.Error != nil {
		resp["error"] = map[string]string{"name": status.Error.Name, "message": status.Error.Message}
	}
	if !status.Output.IsUndefined() && !status.Output.IsNull() {
		s := js.Global().Get("JSON").Call("stringify", status.Output).String()
		resp["output"] = json.RawMessage(s)
	}
	return resp
}
