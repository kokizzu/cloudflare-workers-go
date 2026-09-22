//go:build js && wasm

package kv

import (
	"testing"

	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"
)

func TestGetOptions_toKVJS(t *testing.T) {
	tests := map[string]struct {
		opts  *GetOptions
		type_ string
		want  kvjs.KVNamespaceGetOptions
	}{
		"nil_text":   {opts: nil, type_: "text", want: kvjs.KVNamespaceGetOptions{Type: "text"}},
		"nil_stream": {opts: nil, type_: "stream", want: kvjs.KVNamespaceGetOptions{Type: "stream"}},
		"cache_ttl":  {opts: &GetOptions{CacheTTL: 60}, type_: "text", want: kvjs.KVNamespaceGetOptions{Type: "text", CacheTTL: 60}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toKVJS(tt.type_)
			if got != tt.want {
				t.Errorf("toKVJS(%q) = %+v, want %+v", tt.type_, got, tt.want)
			}
		})
	}
}

func TestPutOptions_toKVJS(t *testing.T) {
	// kvjs.KVNamespacePutOptions embeds a js.Value (Metadata), which
	// syscall/js deliberately makes uncomparable with ==, so this checks
	// the Expiration/ExpirationTTL fields individually rather than
	// comparing the whole struct.
	tests := map[string]struct {
		opts              *PutOptions
		wantExpiration    int
		wantExpirationTTL int
	}{
		"nil":            {opts: nil},
		"expiration":     {opts: &PutOptions{Expiration: 10}, wantExpiration: 10},
		"expiration_ttl": {opts: &PutOptions{ExpirationTTL: 60}, wantExpirationTTL: 60},
		"both":           {opts: &PutOptions{Expiration: 10, ExpirationTTL: 60}, wantExpiration: 10, wantExpirationTTL: 60},
		"zero_value":     {opts: &PutOptions{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toKVJS()
			if got.Expiration != tt.wantExpiration {
				t.Errorf("Expiration = %d, want %d", got.Expiration, tt.wantExpiration)
			}
			if got.ExpirationTTL != tt.wantExpirationTTL {
				t.Errorf("ExpirationTTL = %d, want %d", got.ExpirationTTL, tt.wantExpirationTTL)
			}
		})
	}
}

func TestListOptions_toKVJS(t *testing.T) {
	tests := map[string]struct {
		opts *ListOptions
		want kvjs.KVNamespaceListOptions
	}{
		"nil":        {opts: nil, want: kvjs.KVNamespaceListOptions{}},
		"limit":      {opts: &ListOptions{Limit: 5}, want: kvjs.KVNamespaceListOptions{Limit: 5}},
		"prefix":     {opts: &ListOptions{Prefix: "foo/"}, want: kvjs.KVNamespaceListOptions{Prefix: "foo/"}},
		"cursor":     {opts: &ListOptions{Cursor: "abc"}, want: kvjs.KVNamespaceListOptions{Cursor: "abc"}},
		"all":        {opts: &ListOptions{Limit: 5, Prefix: "foo/", Cursor: "abc"}, want: kvjs.KVNamespaceListOptions{Limit: 5, Prefix: "foo/", Cursor: "abc"}},
		"zero_value": {opts: &ListOptions{}, want: kvjs.KVNamespaceListOptions{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.opts.toKVJS()
			if got != tt.want {
				t.Errorf("toKVJS() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
