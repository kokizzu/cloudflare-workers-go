//go:build js && wasm

package cron

import (
	"context"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/runtimecontext"
)

func TestNewEvent(t *testing.T) {
	t.Run("second_precision", func(t *testing.T) {
		// scheduledTime falls on a whole second (no milliseconds), which
		// event.go's time.UnixMilli handles the same as before.
		obj := js.ValueOf(map[string]any{
			"cron":          "* * * * *",
			"scheduledTime": 1700000000000.0,
		})
		ctx := runtimecontext.New(context.Background(), obj)

		got, err := NewEvent(ctx)
		if err != nil {
			t.Fatalf("NewEvent() error = %v", err)
		}
		if got.Cron != "* * * * *" {
			t.Errorf("Cron = %q, want %q", got.Cron, "* * * * *")
		}
		want := time.Unix(1700000000, 0).UTC()
		if !got.ScheduledTime.Equal(want) {
			t.Errorf("ScheduledTime = %v, want %v", got.ScheduledTime, want)
		}
	})

	t.Run("milliseconds_preserved", func(t *testing.T) {
		obj := js.ValueOf(map[string]any{
			"cron":          "*/5 * * * *",
			"scheduledTime": 1700000000123.0,
		})
		ctx := runtimecontext.New(context.Background(), obj)

		got, err := NewEvent(ctx)
		if err != nil {
			t.Fatalf("NewEvent() error = %v", err)
		}
		want := time.UnixMilli(1700000000123).UTC()
		if !got.ScheduledTime.Equal(want) {
			t.Errorf("ScheduledTime = %v, want %v", got.ScheduledTime, want)
		}
	})
}

// TestNewEvent_missingTrigger fixes the current behavior: NewEvent relies on
// runtimecontext.MustExtractTriggerObj, which panics (rather than returning
// an error) when ctx was not produced by runtimecontext.New.
func TestNewEvent_missingTrigger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewEvent() did not panic, want panic (ctx has no trigger object)")
		}
	}()

	_, _ = NewEvent(context.Background())
	t.Fatal("unreachable: NewEvent() should have panicked")
}
