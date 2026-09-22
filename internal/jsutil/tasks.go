package jsutil

import "sync"

// backgroundTasks tracks goroutines spawned for extended-lifetime work
// (e.g. cloudflare.WaitUntil tasks) so the program does not exit while
// they are still running. Under js/wasm, the Go program returning marks
// the whole instance as exited; resuming a parked task goroutine from a
// timer afterwards fails with "Go program has already exited".
var backgroundTasks sync.WaitGroup

// TrackBackgroundTask runs fn in a new goroutine, registered as a
// background task. WaitBackgroundTasks blocks until every registered task
// has returned.
func TrackBackgroundTask(fn func()) {
	backgroundTasks.Add(1)
	go func() {
		defer backgroundTasks.Done()
		fn()
	}()
}

// WaitBackgroundTasks blocks until all tasks registered via
// TrackBackgroundTask so far have returned.
func WaitBackgroundTasks() {
	backgroundTasks.Wait()
}
