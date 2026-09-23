//go:build js && wasm

package jsutil

import (
	"testing"
	"time"
)

func TestTrackBackgroundTask(t *testing.T) {
	done := make(chan struct{})
	TrackBackgroundTask(func() { close(done) })

	waited := make(chan struct{})
	go func() {
		WaitBackgroundTasks()
		close(waited)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tracked task did not run")
	}
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitBackgroundTasks did not return after the task completed")
	}
}

func TestWaitBackgroundTasks_waitsForSleepingTask(t *testing.T) {
	TrackBackgroundTask(func() {
		time.Sleep(100 * time.Millisecond)
	})

	waited := make(chan struct{})
	go func() {
		WaitBackgroundTasks()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("WaitBackgroundTasks returned while the task was still sleeping")
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitBackgroundTasks did not return after the task completed")
	}
}
