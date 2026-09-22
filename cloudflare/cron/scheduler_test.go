package cron

import (
	"context"
	"errors"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// awaitRejected waits for p to settle and fails the test if it does not
// settle within 5 seconds or if it resolves instead of rejecting. It
// mirrors the root package's handler_js_test.go helper of the same name;
// duplicated here since there is no shared test-helper package for it (see
// jstest.Await, which only handles the resolve path).
func awaitRejected(t testing.TB, p js.Value) error {
	t.Helper()
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := jsutil.AwaitPromise(p)
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		if r.err == nil {
			t.Fatalf("promise resolved with %v, want it to reject", r.v)
		}
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out after 5s waiting for promise to reject")
		return nil
	}
}

// TestRunScheduler_callsTask verifies that runScheduler (registered on
// jsutil.Binding as "runScheduler" by this package's init) invokes the task
// set via ScheduleTaskNonBlock with a context.Context from which
// NewEvent can extract the same Cron expression and ScheduledTime that were
// on the ScheduledEvent object passed in.
func TestRunScheduler_callsTask(t *testing.T) {
	const cronExpr = "* * * * *"
	const scheduledTimeMs = 1700000000000

	var gotEvent *Event
	ScheduleTaskNonBlock(func(ctx context.Context) error {
		ev, err := NewEvent(ctx)
		if err != nil {
			t.Errorf("NewEvent: %v", err)
			return nil
		}
		gotEvent = ev
		return nil
	})
	t.Cleanup(func() { scheduledTask = nil })

	eventObj := jsutil.NewObject()
	eventObj.Set("cron", cronExpr)
	eventObj.Set("scheduledTime", js.ValueOf(float64(scheduledTimeMs)))

	p := jstest.Binding(t, "runScheduler").Invoke(eventObj)
	jstest.Await(t, p)

	if gotEvent == nil {
		t.Fatalf("task was not called")
	}
	if gotEvent.Cron != cronExpr {
		t.Errorf("Cron = %q, want %q", gotEvent.Cron, cronExpr)
	}
	wantTime := time.Unix(scheduledTimeMs/1000, 0).UTC()
	if !gotEvent.ScheduledTime.Equal(wantTime) {
		t.Errorf("ScheduledTime = %v, want %v", gotEvent.ScheduledTime, wantTime)
	}
}

// TestRunScheduler_taskError verifies that a task returning an error
// rejects the Promise returned by the "runScheduler" binding, instead of
// crashing the wasm process. runScheduler is now registered via
// jsutil.RegisterAsyncHandler (see this package's init), which recovers a
// panic and rejects on a non-nil error return, unlike the bespoke Promise
// executor this used to be wired up through.
func TestRunScheduler_taskError(t *testing.T) {
	wantErr := errors.New("boom")
	ScheduleTaskNonBlock(func(context.Context) error { return wantErr })
	t.Cleanup(func() { scheduledTask = nil })

	eventObj := jsutil.NewObject()
	eventObj.Set("cron", "* * * * *")
	eventObj.Set("scheduledTime", js.ValueOf(float64(0)))

	p := jstest.Binding(t, "runScheduler").Invoke(eventObj)
	err := awaitRejected(t, p)
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q, want it to contain %q", err, "boom")
	}
}

// TestRunScheduler_beforeSchedule verifies that invoking the "runScheduler"
// binding before ScheduleTask/ScheduleTaskNonBlock has set scheduledTask
// (so runScheduler calls a nil Task, a nil pointer dereference) rejects the
// Promise instead of crashing the wasm process, now that runScheduler's
// panic is recovered by jsutil.RegisterAsyncHandler.
func TestRunScheduler_beforeSchedule(t *testing.T) {
	scheduledTask = nil

	eventObj := jsutil.NewObject()
	eventObj.Set("cron", "* * * * *")
	eventObj.Set("scheduledTime", js.ValueOf(float64(0)))

	p := jstest.Binding(t, "runScheduler").Invoke(eventObj)
	if err := awaitRejected(t, p); !strings.Contains(err.Error(), "nil pointer") {
		t.Errorf("error = %q, want it to mention a nil pointer dereference", err)
	}
}

// TestScheduleTask_blocks verifies that ScheduleTask calls Ready() and then
// blocks forever: Done() is documented as never closing, to support the
// cloudflare.WaitUntil feature. It runs ScheduleTask in a goroutine that is
// intentionally never unblocked - that goroutine leaks for the rest of the
// test binary's process, which is fine since the process exits once the
// package's tests finish.
func TestScheduleTask_blocks(t *testing.T) {
	before := jstest.ReadyCount(t)
	go func() {
		ScheduleTask(func(context.Context) error { return nil })
	}()

	deadline := time.Now().Add(5 * time.Second)
	for jstest.ReadyCount(t)-before != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("ScheduleTask did not call Ready() within 5s")
		}
		time.Sleep(time.Millisecond)
	}
}
