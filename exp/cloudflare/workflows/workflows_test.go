//go:build js && wasm

package workflows

import (
	"syscall/js"
	"testing"
)

// TestWorkflow_Create exercises a handle-typed binding whose method takes a
// data-type argument (WorkflowInstanceCreateOptions) and resolves to
// another handle type (WorkflowInstance): a fake JS Workflow object whose
// create() returns a resolved Promise wrapping a fake WorkflowInstance,
// wrapped with WorkflowFromJS, called through Create.
func TestWorkflow_Create(t *testing.T) {
	var gotID string
	fakeInstance := js.ValueOf(map[string]any{"id": "instance-1"})
	fake := js.ValueOf(map[string]any{})
	fake.Set("create", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotID = args[0].Get("id").String()
		return js.Global().Get("Promise").Call("resolve", fakeInstance)
	}))

	wf := WorkflowFromJS(fake)
	instance, err := wf.Create(WorkflowInstanceCreateOptions{ID: "my-instance"})
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if instance.ID() != "instance-1" {
		t.Errorf("instance.ID() = %q, want %q", instance.ID(), "instance-1")
	}
	if gotID != "my-instance" {
		t.Errorf("options.id sent to JS = %q, want %q", gotID, "my-instance")
	}
}

// TestWorkflowInstance_Status exercises decoding a data type (InstanceStatus)
// off a Promise-returning method.
func TestWorkflowInstance_Status(t *testing.T) {
	fake := js.ValueOf(map[string]any{"id": "instance-1"})
	fake.Set("status", js.FuncOf(func(this js.Value, args []js.Value) any {
		result := js.ValueOf(map[string]any{"status": "running"})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	instance := WorkflowInstanceFromJS(fake)
	status, err := instance.Status()
	if err != nil {
		t.Fatalf("Status() failed: %v", err)
	}
	if status.Status != "running" {
		t.Errorf("status.Status = %q, want %q", status.Status, "running")
	}
}

// TestWorkflow_DeleteBatch exercises an array-typed method parameter
// (instanceIds []string), which requires cfgen's multi-statement JS
// conversion support.
func TestWorkflow_DeleteBatch(t *testing.T) {
	var gotLen int
	fake := js.ValueOf(map[string]any{})
	fake.Set("deleteBatch", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotLen = args[0].Length()
		result := js.ValueOf(map[string]any{
			"deleted": []any{map[string]any{"id": "a"}},
			"errors":  []any{},
		})
		return js.Global().Get("Promise").Call("resolve", result)
	}))

	wf := WorkflowFromJS(fake)
	result, err := wf.DeleteBatch([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("DeleteBatch() failed: %v", err)
	}
	if gotLen != 3 {
		t.Errorf("instanceIds sent to JS had length %d, want 3", gotLen)
	}
	if len(result.Deleted) != 1 {
		t.Errorf("len(result.Deleted) = %d, want 1", len(result.Deleted))
	}
}

// TestWorkflow_CreateBatch exercises an array-of-data-type method parameter
// resolving to an array-of-handle-type return ([]*WorkflowInstance): the
// tmp/06-codegen-spec.md 6.2 item 1 re-enable of Workflow.createBatch, which
// was previously excluded from generation.
func TestWorkflow_CreateBatch(t *testing.T) {
	var gotIDs []string
	fake := js.ValueOf(map[string]any{})
	fake.Set("createBatch", js.FuncOf(func(this js.Value, args []js.Value) any {
		batch := args[0]
		for i := 0; i < batch.Length(); i++ {
			gotIDs = append(gotIDs, batch.Index(i).Get("id").String())
		}
		instances := js.Global().Get("Array").New(batch.Length())
		for i := 0; i < batch.Length(); i++ {
			instances.SetIndex(i, js.ValueOf(map[string]any{"id": batch.Index(i).Get("id").String()}))
		}
		return js.Global().Get("Promise").Call("resolve", instances)
	}))

	wf := WorkflowFromJS(fake)
	instances, err := wf.CreateBatch([]WorkflowInstanceCreateOptions{
		{ID: "a"},
		{ID: "b"},
	})
	if err != nil {
		t.Fatalf("CreateBatch() failed: %v", err)
	}
	if want := []string{"a", "b"}; len(gotIDs) != len(want) || gotIDs[0] != want[0] || gotIDs[1] != want[1] {
		t.Errorf("options ids sent to JS = %v, want %v", gotIDs, want)
	}
	if len(instances) != 2 {
		t.Fatalf("len(instances) = %d, want 2", len(instances))
	}
	if instances[0].ID() != "a" || instances[1].ID() != "b" {
		t.Errorf("instances = [%q, %q], want [\"a\", \"b\"]", instances[0].ID(), instances[1].ID())
	}
}

// TestWorkflowInstance_SendEvent exercises the tmp/06-codegen-spec.md 6.2
// item 1 re-enable of WorkflowInstance.sendEvent, whose single parameter is
// a destructured object literal ({ type, payload }) in the .d.ts: the IR
// records its raw destructuring-pattern source text as the parameter's
// "name", which cfgen's param-rename override (in
// exp/internal/gen/overrides/workflows.yaml) turns into an ordinary Go
// parameter named event of the synthesized WorkflowInstanceSendEventEvent
// type.
func TestWorkflowInstance_SendEvent(t *testing.T) {
	var gotType string
	var gotPayload string
	fake := js.ValueOf(map[string]any{"id": "instance-1"})
	fake.Set("sendEvent", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotType = args[0].Get("type").String()
		gotPayload = args[0].Get("payload").String()
		return js.Global().Get("Promise").Call("resolve", js.Undefined())
	}))

	instance := WorkflowInstanceFromJS(fake)
	err := instance.SendEvent(WorkflowInstanceSendEventEvent{
		Type:    "my-event",
		Payload: js.ValueOf("my-payload"),
	})
	if err != nil {
		t.Fatalf("SendEvent() failed: %v", err)
	}
	if gotType != "my-event" {
		t.Errorf("event.type sent to JS = %q, want %q", gotType, "my-event")
	}
	if gotPayload != "my-payload" {
		t.Errorf("event.payload sent to JS = %q, want %q", gotPayload, "my-payload")
	}
}
