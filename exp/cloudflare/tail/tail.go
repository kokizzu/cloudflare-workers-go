//go:build js && wasm

package tail

import (
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// Handler handles one batch of trace events delivered to a Worker's
// tail(events, env, ctx) handler — i.e. a Tail Worker configured as a
// `tail_consumers` entry of another ("producer") Worker
// (https://developers.cloudflare.com/workers/observability/logs/tail-workers/).
// A returned error rejects the underlying Promise; Cloudflare logs the
// rejection but does not retry delivery (there is no equivalent of a queue
// consumer's automatic retry here).
type Handler func(items []TraceItem) error

//go:wasmimport workers ready
func ready()

// Handle registers h as the Worker's tail handler and blocks until the
// Worker instance is torn down. It is meant for a Worker whose only job is
// consuming trace events (see _examples/tail-worker); a Worker that also
// serves other triggers should register those first, then call a single
// blocking entry point (such as workers.Serve) instead of calling Handle.
func Handle(h Handler) {
	registerHandler(h)
	ready()
	select {}
}

// registerHandler wires h up to the "handleTail" JS-callable binding
// worker.mjs's tail(events, env, ctx) export calls, without touching
// ready/select{} — split out from Handle so tests can register a handler
// and drive it directly through jsutil.Binding without also pulling in
// (and blocking forever on) Handle's ready()/select{} tail.
func registerHandler(h Handler) {
	jsutil.RegisterAsyncHandler("handleTail", 1, func(args []js.Value) (js.Value, error) {
		v := args[0]
		items := make([]TraceItem, v.Length())
		for i := range items {
			item, err := traceItemFromJS(v.Index(i))
			if err != nil {
				return js.Undefined(), err
			}
			items[i] = item
		}
		return js.Undefined(), h(items)
	})
}

// EventKind identifies the shape of Event, inferred from which of the
// TraceItem.event union's ten branches' distinguishing properties are
// present on the raw object (Event is generated as js.Value — see
// tail.yaml's doc comment for why cfgen can't synthesize a single Go type
// for this union). One of "fetch", "jsrpc", "scheduled", "alarm", "queue",
// "email", "tail", or "hibernatableWebSocket" is returned for the branches
// that carry at least one distinguishing property; "connect" and "custom"
// (TraceItemConnectEventInfo/TraceItemCustomEventInfo) carry none of their
// own and so can't be told apart from each other, or from a null/undefined
// Event, this way — both, and any other unrecognized shape, report "".
func (item TraceItem) EventKind() string {
	v := item.Event
	switch {
	case hasProp(v, "request"):
		return "fetch"
	case hasProp(v, "rpcMethod"):
		return "jsrpc"
	case hasProp(v, "cron"):
		return "scheduled"
	case hasProp(v, "queue"):
		return "queue"
	case hasProp(v, "mailFrom"):
		return "email"
	case hasProp(v, "consumedEvents"):
		return "tail"
	case hasProp(v, "getWebSocketEvent"):
		return "hibernatableWebSocket"
	case hasProp(v, "scheduledTime"):
		return "alarm"
	default:
		return ""
	}
}

// hasProp reports whether v has an own or inherited property named name
// that isn't undefined. v itself may be the zero js.Value (undefined) or
// null, in which case it always reports false.
func hasProp(v js.Value, name string) bool {
	if v.IsUndefined() || v.IsNull() {
		return false
	}
	return !v.Get(name).IsUndefined()
}

// EventTime decodes EventTimestamp (a Unix millisecond timestamp) as a
// time.Time, and true, if it is non-zero. EventTimestamp is generated as a
// plain float64 rather than a pointer (see tail.yaml's doc comment: cfgen
// has no "*float64" types: override, and TraceItem retains no underlying
// js.Value to re-check for null after decoding), so an EventTimestamp of
// exactly 0 — indistinguishable from "absent" this way — is reported as
// absent too.
func (item TraceItem) EventTime() (time.Time, bool) {
	if item.EventTimestamp == 0 {
		return time.Time{}, false
	}
	ms := item.EventTimestamp
	return time.UnixMilli(int64(ms)), true
}

// FetchEvent returns Event decoded as a *TraceItemFetchEventInfo, and true,
// if EventKind() == "fetch". Otherwise it returns nil, false.
func (item TraceItem) FetchEvent() (*TraceItemFetchEventInfo, bool) {
	if item.EventKind() != "fetch" {
		return nil, false
	}
	v, err := traceItemFetchEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// JsRpcEvent returns Event decoded as a *TraceItemJsRpcEventInfo, and true,
// if EventKind() == "jsrpc". Otherwise it returns nil, false.
func (item TraceItem) JsRpcEvent() (*TraceItemJsRpcEventInfo, bool) {
	if item.EventKind() != "jsrpc" {
		return nil, false
	}
	v, err := traceItemJsRpcEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// ScheduledEvent returns Event decoded as a *TraceItemScheduledEventInfo,
// and true, if EventKind() == "scheduled". Otherwise it returns nil, false.
func (item TraceItem) ScheduledEvent() (*TraceItemScheduledEventInfo, bool) {
	if item.EventKind() != "scheduled" {
		return nil, false
	}
	v, err := traceItemScheduledEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// AlarmEvent returns Event decoded as a *TraceItemAlarmEventInfo, and true,
// if EventKind() == "alarm". Otherwise it returns nil, false.
func (item TraceItem) AlarmEvent() (*TraceItemAlarmEventInfo, bool) {
	if item.EventKind() != "alarm" {
		return nil, false
	}
	v, err := traceItemAlarmEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// QueueEvent returns Event decoded as a *TraceItemQueueEventInfo, and true,
// if EventKind() == "queue". Otherwise it returns nil, false.
func (item TraceItem) QueueEvent() (*TraceItemQueueEventInfo, bool) {
	if item.EventKind() != "queue" {
		return nil, false
	}
	v, err := traceItemQueueEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// EmailEvent returns Event decoded as a *TraceItemEmailEventInfo, and true,
// if EventKind() == "email". Otherwise it returns nil, false.
func (item TraceItem) EmailEvent() (*TraceItemEmailEventInfo, bool) {
	if item.EventKind() != "email" {
		return nil, false
	}
	v, err := traceItemEmailEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// TailEvent returns Event decoded as a *TraceItemTailEventInfo, and true,
// if EventKind() == "tail" (this TraceItem itself came from a Tail Worker
// chained after another one). Otherwise it returns nil, false.
func (item TraceItem) TailEvent() (*TraceItemTailEventInfo, bool) {
	if item.EventKind() != "tail" {
		return nil, false
	}
	v, err := traceItemTailEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}

// HibernatableWebSocketEvent returns Event decoded as a
// *TraceItemHibernatableWebSocketEventInfo, and true, if
// EventKind() == "hibernatableWebSocket". Otherwise it returns nil, false.
func (item TraceItem) HibernatableWebSocketEvent() (*TraceItemHibernatableWebSocketEventInfo, bool) {
	if item.EventKind() != "hibernatableWebSocket" {
		return nil, false
	}
	v, err := traceItemHibernatableWebSocketEventInfoFromJS(item.Event)
	if err != nil {
		return nil, false
	}
	return &v, true
}
