//go:build js && wasm

package durableobjects

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// resetInstance clears every package-level Register/instance state (the
// registered Constructor map, and the mutex-guarded instance/state this
// package memoizes per wasm instance) around a test, so tests don't leak
// into each other — in production, all of this state is scoped to one wasm
// instance's lifetime (see the package doc comment), but a `go test` binary
// keeps running across many "instances" worth of test cases.
func resetInstance(t *testing.T) {
	t.Helper()
	constructorsMu.Lock()
	prevConstructors := constructors
	constructors = map[string]Constructor{}
	constructorsMu.Unlock()

	instanceMu.Lock()
	prevReady := instanceReady
	prevObj := instanceObj
	prevState := instanceState
	instanceReady = false
	instanceObj = nil
	instanceState = nil
	instanceMu.Unlock()

	t.Cleanup(func() {
		constructorsMu.Lock()
		constructors = prevConstructors
		constructorsMu.Unlock()
		instanceMu.Lock()
		instanceReady = prevReady
		instanceObj = prevObj
		instanceState = prevState
		instanceMu.Unlock()
	})
}

// withRuntimeContext installs a fake jsutil.RuntimeContext of the shape
// worker.mjs's GoDurableObject#bind builds for a Durable Object trigger
// (durableObject: {className}, ctx, env), restoring the previous one via
// t.Cleanup — mirroring exp/cloudflare/email/email_test.go's
// withEmailMessageClass and exp/cloudflare/websocket/websocket_test.go's
// fakes. jsutil.RuntimeContext is a package variable, not something this
// package's own tests can set up any other way.
func withRuntimeContext(t *testing.T, className string, ctxVal, envVal js.Value) {
	t.Helper()
	prev := jsutil.RuntimeContext
	rc := jsrt.NewObject()
	do := jsrt.NewObject()
	do.Set("className", className)
	rc.Set("durableObject", do)
	rc.Set("ctx", ctxVal)
	rc.Set("env", envVal)
	jsutil.RuntimeContext = rc
	t.Cleanup(func() { jsutil.RuntimeContext = prev })
}

// fakeRequest builds a real JS Request object (Node's global Request class,
// the same one jsutil.RequestClass points at) good enough for
// jshttp.ToRequest/ServeRequest to decode.
func fakeRequest(path string) js.Value {
	return jsutil.RequestClass.New("http://counter.test"+path, js.ValueOf(map[string]any{
		"method": "GET",
	}))
}

// countingObject is a durableobjects.Object (and AlarmHandler) that counts
// how many times it was constructed (via newCountingObject) and how many
// times ServeHTTP/Alarm were invoked, so tests can tell whether the same
// instance was reused across triggers.
type countingObject struct {
	id       int
	requests int

	mu         sync.Mutex
	alarmCalls int
	lastAlarm  *AlarmInvocationInfo
}

func newCountingObject(constructCount *int) Constructor {
	return func(state *DurableObjectState, env js.Value) (Object, error) {
		*constructCount++
		return &countingObject{id: *constructCount}, nil
	}
}

func (o *countingObject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.requests++
	fmt.Fprintf(w, "id=%d requests=%d", o.id, o.requests)
}

func (o *countingObject) Alarm(ctx context.Context, info *AlarmInvocationInfo) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.alarmCalls++
	o.lastAlarm = info
	return nil
}

func bodyText(t *testing.T, resp js.Value) string {
	t.Helper()
	v, err := jsrt.Await(resp.Call("text"))
	if err != nil {
		t.Fatalf("resp.text() failed: %v", err)
	}
	return v.String()
}

// TestHandleDurableObjectFetch_ConstructsOnceAndReusesInstance verifies
// that Register's Constructor is invoked exactly once for a wasm instance,
// and that two handleDurableObjectFetch triggers dispatch to that same
// Object instance (its own request counter goes from 1 to 2), matching the
// "one Durable Object instance per wasm instance, reused across triggers"
// model from tmp/06-codegen-spec.md 4.0/4.2.
func TestHandleDurableObjectFetch_ConstructsOnceAndReusesInstance(t *testing.T) {
	resetInstance(t)
	withRuntimeContext(t, "Counter", js.ValueOf(map[string]any{}), js.ValueOf(map[string]any{}))

	var constructCount int
	Register("Counter", newCountingObject(&constructCount))

	handleFetch := jsutil.Binding.Get("handleDurableObjectFetch")
	if handleFetch.IsUndefined() {
		t.Fatal("init() did not register \"handleDurableObjectFetch\" on jsutil.Binding")
	}

	resp1, err := jsrt.Await(handleFetch.Invoke(fakeRequest("/")))
	if err != nil {
		t.Fatalf("handleDurableObjectFetch (1st call) rejected: %v", err)
	}
	resp2, err := jsrt.Await(handleFetch.Invoke(fakeRequest("/")))
	if err != nil {
		t.Fatalf("handleDurableObjectFetch (2nd call) rejected: %v", err)
	}

	if constructCount != 1 {
		t.Fatalf("Constructor called %d times, want 1", constructCount)
	}
	if got, want := bodyText(t, resp1), "id=1 requests=1"; got != want {
		t.Errorf("1st response body = %q, want %q", got, want)
	}
	if got, want := bodyText(t, resp2), "id=1 requests=2"; got != want {
		t.Errorf("2nd response body = %q, want %q (same instance, 2nd request)", got, want)
	}
}

// TestHandleDurableObjectFetch_UnregisteredClassName verifies that a
// className with no Registered Constructor rejects the Promise instead of
// panicking or silently doing nothing.
func TestHandleDurableObjectFetch_UnregisteredClassName(t *testing.T) {
	resetInstance(t)
	withRuntimeContext(t, "NotRegistered", js.ValueOf(map[string]any{}), js.ValueOf(map[string]any{}))

	handleFetch := jsutil.Binding.Get("handleDurableObjectFetch")
	promise := handleFetch.Invoke(fakeRequest("/"))
	if _, err := jsrt.Await(promise); err == nil {
		t.Fatal("handleDurableObjectFetch resolved for an unregistered class name, want a rejection")
	}
}

// TestHandleDurableObjectFetch_RetriesConstructorAfterError verifies that a
// Constructor error is not cached: the next trigger delivered to the same
// wasm instance retries construction from scratch instead of failing
// forever (see Constructor's doc comment on why a failed attempt isn't
// memoized).
func TestHandleDurableObjectFetch_RetriesConstructorAfterError(t *testing.T) {
	resetInstance(t)
	withRuntimeContext(t, "Counter", js.ValueOf(map[string]any{}), js.ValueOf(map[string]any{}))

	var attempts int
	Register("Counter", func(state *DurableObjectState, env js.Value) (Object, error) {
		attempts++
		if attempts == 1 {
			return nil, fmt.Errorf("transient failure")
		}
		return &countingObject{id: attempts}, nil
	})

	handleFetch := jsutil.Binding.Get("handleDurableObjectFetch")
	if _, err := jsrt.Await(handleFetch.Invoke(fakeRequest("/"))); err == nil {
		t.Fatal("handleDurableObjectFetch (1st call) resolved, want a rejection from the failing Constructor")
	}
	resp, err := jsrt.Await(handleFetch.Invoke(fakeRequest("/")))
	if err != nil {
		t.Fatalf("handleDurableObjectFetch (2nd call) rejected: %v, want the retried Constructor to succeed", err)
	}

	if attempts != 2 {
		t.Fatalf("Constructor called %d times, want 2 (1 failure + 1 retry)", attempts)
	}
	if got, want := bodyText(t, resp), "id=2 requests=1"; got != want {
		t.Errorf("response body = %q, want %q", got, want)
	}
}

// TestHandleDurableObjectAlarm_DispatchesToAlarmHandler verifies that
// handleDurableObjectAlarm decodes the JS AlarmInvocationInfo argument and
// dispatches it to the instance's AlarmHandler.Alarm.
func TestHandleDurableObjectAlarm_DispatchesToAlarmHandler(t *testing.T) {
	resetInstance(t)
	withRuntimeContext(t, "Counter", js.ValueOf(map[string]any{}), js.ValueOf(map[string]any{}))

	var constructCount int
	var obj *countingObject
	Register("Counter", func(state *DurableObjectState, env js.Value) (Object, error) {
		constructCount++
		obj = &countingObject{id: constructCount}
		return obj, nil
	})

	handleAlarm := jsutil.Binding.Get("handleDurableObjectAlarm")
	if handleAlarm.IsUndefined() {
		t.Fatal("init() did not register \"handleDurableObjectAlarm\" on jsutil.Binding")
	}

	info := js.ValueOf(map[string]any{
		"isRetry":       true,
		"retryCount":    2.0,
		"scheduledTime": 12345.0,
	})
	if _, err := jsrt.Await(handleAlarm.Invoke(info)); err != nil {
		t.Fatalf("handleDurableObjectAlarm rejected: %v", err)
	}

	if obj.alarmCalls != 1 {
		t.Fatalf("Alarm called %d times, want 1", obj.alarmCalls)
	}
	if obj.lastAlarm == nil {
		t.Fatal("Alarm was called with a nil *AlarmInvocationInfo")
	}
	if !obj.lastAlarm.IsRetry || obj.lastAlarm.RetryCount != 2 || obj.lastAlarm.ScheduledTime != 12345 {
		t.Errorf("Alarm got %+v, want {IsRetry:true RetryCount:2 ScheduledTime:12345}", *obj.lastAlarm)
	}
}
