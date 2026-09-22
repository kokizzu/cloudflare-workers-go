//go:build js && wasm

package queues

import (
	"testing"

	queuesjs "github.com/syumai/workers-go/exp/cloudflare/queues"
)

// TestSendOptions_toQueuesJS fixes sendOptions.toQueuesJS's current
// behavior: it maps every field straight through to the generated
// queuesjs.QueueSendOptions struct (no nil guard is needed since it takes
// sendOptions by value, and every caller in this package builds a
// sendOptions value, never a pointer).
func TestSendOptions_toQueuesJS(t *testing.T) {
	tests := map[string]struct {
		opts sendOptions
	}{
		"zero_value": {
			opts: sendOptions{},
		},
		"content_type_only": {
			opts: sendOptions{ContentType: contentTypeText},
		},
		"with_delay": {
			opts: sendOptions{ContentType: contentTypeJSON, DelaySeconds: 5},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toQueuesJS()
			if got.ContentType != queuesjs.QueueContentType(tc.opts.ContentType) {
				t.Errorf("ContentType = %q, want %q", got.ContentType, tc.opts.ContentType)
			}
			if got.DelaySeconds != tc.opts.DelaySeconds {
				t.Errorf("DelaySeconds = %v, want %v", got.DelaySeconds, tc.opts.DelaySeconds)
			}
		})
	}
}

// TestBatchSendOptions_toQueuesJS fixes batchSendOptions.toQueuesJS's nil
// guard: a nil *batchSendOptions must convert to a zero-value
// queuesjs.QueueSendBatchOptions rather than panicking.
func TestBatchSendOptions_toQueuesJS(t *testing.T) {
	tests := map[string]struct {
		opts      *batchSendOptions
		wantDelay int
	}{
		"nil": {
			opts: nil,
		},
		"zero": {
			opts: &batchSendOptions{},
		},
		"delay": {
			opts:      &batchSendOptions{DelaySeconds: 5},
			wantDelay: 5,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toQueuesJS()
			if got.DelaySeconds != tc.wantDelay {
				t.Errorf("DelaySeconds = %v, want %v", got.DelaySeconds, tc.wantDelay)
			}
		})
	}
}

func TestRetryOptions_toJS(t *testing.T) {
	tests := map[string]struct {
		opts      *retryOptions
		wantUndef bool
		wantDelay int
		wantOmit  bool
	}{
		"nil": {
			opts:      nil,
			wantUndef: true,
		},
		"zero_omitted": {
			opts:     &retryOptions{},
			wantOmit: true,
		},
		"delay": {
			opts:      &retryOptions{delaySeconds: 10},
			wantDelay: 10,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toJS()
			if tc.wantUndef {
				if !got.IsUndefined() {
					t.Fatalf("toJS() = %v, want undefined", got)
				}
				return
			}
			delay := got.Get("delaySeconds")
			if tc.wantOmit {
				if !delay.IsUndefined() {
					t.Errorf("delaySeconds = %v, want undefined", delay)
				}
				return
			}
			if delay.Int() != tc.wantDelay {
				t.Errorf("delaySeconds = %v, want %v", delay.Int(), tc.wantDelay)
			}
		})
	}
}

func TestContentType(t *testing.T) {
	tests := map[string]struct {
		ct   contentType
		want string
	}{
		"json":  {ct: contentTypeJSON, want: "json"},
		"v8":    {ct: contentTypeV8, want: "v8"},
		"text":  {ct: contentTypeText, want: "text"},
		"bytes": {ct: contentTypeBytes, want: "bytes"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := string(tc.ct); got != tc.want {
				t.Errorf("string(%s) = %q, want %q", name, got, tc.want)
			}
		})
	}
}
