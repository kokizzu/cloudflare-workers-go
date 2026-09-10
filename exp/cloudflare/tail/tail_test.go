//go:build js && wasm

package tail

import (
	"errors"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/exp/internal/jsrt"
	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeTraceItem builds a JS object shaped enough like a real TraceItem
// (every field traceItemFromJS reads without an undefined/null guard) for
// decoding to succeed, with event set to the given raw event object and any
// extra fields overlaid on top.
func fakeTraceItem(event js.Value, extra map[string]any) js.Value {
	m := map[string]any{
		"event":                    event,
		"logs":                     []any{},
		"exceptions":               []any{},
		"diagnosticsChannelEvents": []any{},
		"outcome":                  "ok",
		"executionModel":           "stateless",
		"truncated":                false,
		"cpuTime":                  1.5,
		"wallTime":                 2.5,
	}
	for k, v := range extra {
		m[k] = v
	}
	return js.ValueOf(m)
}

// TestRegisterHandler_DecodesBatch verifies that the function registerHandler
// wires up onto jsutil.Binding.handleTail decodes a JS array of TraceItem
// into []TraceItem and invokes the Handler with it — the same path Handle
// uses, without Handle's ready()/select{} tail.
func TestRegisterHandler_DecodesBatch(t *testing.T) {
	fetchEvent := js.ValueOf(map[string]any{
		"request": map[string]any{
			"headers": map[string]any{},
			"method":  "GET",
			"url":     "https://example.com/",
		},
	})
	item := fakeTraceItem(fetchEvent, map[string]any{"scriptName": "my-worker"})
	batch := js.ValueOf([]any{item})

	var got []TraceItem
	registerHandler(func(items []TraceItem) error {
		got = items
		return nil
	})

	handleTail := jsutil.Binding.Get("handleTail")
	if handleTail.IsUndefined() {
		t.Fatal(`registerHandler did not register "handleTail" on jsutil.Binding`)
	}
	promise := handleTail.Invoke(batch)
	if _, err := jsrt.Await(promise); err != nil {
		t.Fatalf("handleTail promise rejected: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d items, want 1", len(got))
	}
	if got[0].ScriptName != "my-worker" {
		t.Errorf("ScriptName = %q, want %q", got[0].ScriptName, "my-worker")
	}
	if kind := got[0].EventKind(); kind != "fetch" {
		t.Errorf("EventKind() = %q, want %q", kind, "fetch")
	}
	fe, ok := got[0].FetchEvent()
	if !ok {
		t.Fatal("FetchEvent() ok = false, want true")
	}
	if fe.Request == nil || fe.Request.Method() != "GET" {
		t.Errorf("FetchEvent().Request.Method() = %v, want GET", fe.Request)
	}
	if _, ok := got[0].ScheduledEvent(); ok {
		t.Error("ScheduledEvent() ok = true for a fetch event, want false")
	}
}

// TestRegisterHandler_ErrorRejectsPromise verifies a Handler error surfaces
// as a rejected Promise instead of panicking.
func TestRegisterHandler_ErrorRejectsPromise(t *testing.T) {
	batch := js.ValueOf([]any{fakeTraceItem(js.Null(), nil)})

	registerHandler(func(items []TraceItem) error {
		return errors.New("boom")
	})

	promise := jsutil.Binding.Get("handleTail").Invoke(batch)
	if _, err := jsrt.Await(promise); err == nil {
		t.Fatal("handleTail promise resolved, want a rejection")
	}
}

// TestTraceItem_EventKind_NullEvent verifies EventKind returns "" (rather
// than panicking) for a TraceItem whose event is null, the shape a
// TraceItemConnectEventInfo/TraceItemCustomEventInfo or an unrecognized
// event would also produce.
func TestTraceItem_EventKind_NullEvent(t *testing.T) {
	item := TraceItem{Event: js.Null()}
	if kind := item.EventKind(); kind != "" {
		t.Errorf("EventKind() = %q, want \"\"", kind)
	}
	if _, ok := item.FetchEvent(); ok {
		t.Error("FetchEvent() ok = true for a null event, want false")
	}
}

// TestTraceItem_ScheduledVsAlarm verifies EventKind tells
// TraceItemScheduledEventInfo (has "cron") apart from
// TraceItemAlarmEventInfo (only "scheduledTime"), and that the matching
// typed accessor decodes each.
func TestTraceItem_ScheduledVsAlarm(t *testing.T) {
	scheduled := TraceItem{Event: js.ValueOf(map[string]any{
		"cron":          "*/5 * * * *",
		"scheduledTime": 1000.0,
	})}
	if kind := scheduled.EventKind(); kind != "scheduled" {
		t.Fatalf("EventKind() = %q, want %q", kind, "scheduled")
	}
	se, ok := scheduled.ScheduledEvent()
	if !ok {
		t.Fatal("ScheduledEvent() ok = false, want true")
	}
	if se.Cron != "*/5 * * * *" {
		t.Errorf("ScheduledEvent().Cron = %q, want %q", se.Cron, "*/5 * * * *")
	}
	if _, ok := scheduled.AlarmEvent(); ok {
		t.Error("AlarmEvent() ok = true for a scheduled event, want false")
	}

	when := time.Now().Round(time.Millisecond)
	alarm := TraceItem{Event: js.ValueOf(map[string]any{
		"scheduledTime": jsrt.TimeToDate(when),
	})}
	if kind := alarm.EventKind(); kind != "alarm" {
		t.Fatalf("EventKind() = %q, want %q", kind, "alarm")
	}
	ae, ok := alarm.AlarmEvent()
	if !ok {
		t.Fatal("AlarmEvent() ok = false, want true")
	}
	if !ae.ScheduledTime.Equal(when) {
		t.Errorf("AlarmEvent().ScheduledTime = %v, want %v", ae.ScheduledTime, when)
	}
}

// TestTraceItem_EventTime verifies EventTime decodes a non-zero
// EventTimestamp (Unix ms) and reports absent for exactly 0 — see
// EventTime's doc comment for why 0 can't be told apart from "absent".
func TestTraceItem_EventTime(t *testing.T) {
	item := TraceItem{EventTimestamp: 1700000000000}
	got, ok := item.EventTime()
	if !ok {
		t.Fatal("EventTime() ok = false, want true")
	}
	if want := time.UnixMilli(1700000000000); !got.Equal(want) {
		t.Errorf("EventTime() = %v, want %v", got, want)
	}

	zero := TraceItem{EventTimestamp: 0}
	if _, ok := zero.EventTime(); ok {
		t.Error("EventTime() ok = true for a zero EventTimestamp, want false")
	}
}
