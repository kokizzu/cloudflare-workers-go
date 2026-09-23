//go:build js && wasm

package cloudflare

import (
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

func TestWaitUntil(t *testing.T) {
	ec := jstest.NewExecutionContext(t)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: ec.Value()})

	var ran bool
	WaitUntil(func() { ran = true })
	ec.Wait(t)

	if !ran {
		t.Error("task passed to WaitUntil was not run")
	}
}

// TestWaitUntil_sleepingTask verifies that a WaitUntil task which parks
// itself (e.g. in time.Sleep) is tracked as a background task: the task
// must still be counted as running while it sleeps, so that workers.Done
// cannot close - and the Go program cannot exit - before it resumes and
// finishes. Under workerd, resuming such a goroutine after the program
// exited fails with "Go program has already exited".
func TestWaitUntil_sleepingTask(t *testing.T) {
	ec := jstest.NewExecutionContext(t)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: ec.Value()})

	var ran bool
	WaitUntil(func() {
		time.Sleep(100 * time.Millisecond)
		ran = true
	})

	waited := make(chan struct{})
	go func() {
		jsutil.WaitBackgroundTasks()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("WaitBackgroundTasks returned while the WaitUntil task was still sleeping")
	case <-time.After(50 * time.Millisecond):
	}

	ec.Wait(t)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitBackgroundTasks did not return after the WaitUntil task completed")
	}
	if !ran {
		t.Error("task passed to WaitUntil was not run")
	}
}

func TestPassThroughOnException(t *testing.T) {
	ec := jstest.NewExecutionContext(t)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: ec.Value()})

	PassThroughOnException()

	if got, want := ec.PassThroughCalls(), 1; got != want {
		t.Errorf("PassThroughCalls() = %d, want %d", got, want)
	}
}
