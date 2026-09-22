//go:build js && wasm

package cache

import "testing"

// TestMatchOptions_toCacheJS fixes MatchOptions.toCacheJS's current
// behavior: a nil *MatchOptions converts to the zero-value
// cachejs.CacheQueryOptions (IgnoreMethod false) rather than panicking.
func TestMatchOptions_toCacheJS(t *testing.T) {
	tests := map[string]struct {
		opts       *MatchOptions
		wantIgnore bool
	}{
		"nil": {
			opts: nil,
		},
		"ignore_method_false": {
			opts: &MatchOptions{},
		},
		"ignore_method_true": {
			opts:       &MatchOptions{IgnoreMethod: true},
			wantIgnore: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toCacheJS()
			if got.IgnoreMethod != tc.wantIgnore {
				t.Errorf("IgnoreMethod = %v, want %v", got.IgnoreMethod, tc.wantIgnore)
			}
		})
	}
}

// TestDeleteOptions_toCacheJS fixes DeleteOptions.toCacheJS's current
// behavior: a nil *DeleteOptions converts to the zero-value
// cachejs.CacheQueryOptions (IgnoreMethod false) rather than panicking.
func TestDeleteOptions_toCacheJS(t *testing.T) {
	tests := map[string]struct {
		opts       *DeleteOptions
		wantIgnore bool
	}{
		"nil": {
			opts: nil,
		},
		"ignore_method_false": {
			opts: &DeleteOptions{},
		},
		"ignore_method_true": {
			opts:       &DeleteOptions{IgnoreMethod: true},
			wantIgnore: true,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tc.opts.toCacheJS()
			if got.IgnoreMethod != tc.wantIgnore {
				t.Errorf("IgnoreMethod = %v, want %v", got.IgnoreMethod, tc.wantIgnore)
			}
		})
	}
}
