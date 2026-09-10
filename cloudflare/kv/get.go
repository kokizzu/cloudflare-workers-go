package kv

import (
	"errors"
	"io"

	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"
)

// GetOptions represents Cloudflare KV namespace get options.
//   - https://github.com/cloudflare/workers-types/blob/3012f263fb1239825e5f0061b267c8650d01b717/index.d.ts#L930
type GetOptions struct {
	CacheTTL int
}

// toKVJS builds the options for KVNamespace.get/getWithMetadata: the "type"
// discriminant (typ) plus the caller's CacheTTL, if any.
func (opts *GetOptions) toKVJS(typ string) kvjs.KVNamespaceGetOptions {
	o := kvjs.KVNamespaceGetOptions{Type: typ}
	if opts != nil {
		o.CacheTTL = opts.CacheTTL
	}
	return o
}

// ErrNotFound is returned by GetString and GetReader when no value exists
// for the given key.
var ErrNotFound = errors.New("kv: key not found")

// GetString gets string value by the specified key.
//   - if the key doesn't exist, returns ErrNotFound.
//   - if a network error happens, returns error.
func (ns *Namespace) GetString(key string, opts *GetOptions) (string, error) {
	v, err := ns.instance.GetText(key, opts.toKVJS("text"))
	if err != nil {
		return "", err
	}
	if v == nil {
		return "", ErrNotFound
	}
	return *v, nil
}

// GetReader gets stream value by the specified key.
//   - if the key doesn't exist, returns ErrNotFound.
//   - if a network error happens, returns error.
//   - the caller is responsible for closing the returned io.ReadCloser.
func (ns *Namespace) GetReader(key string, opts *GetOptions) (io.ReadCloser, error) {
	v, err := ns.instance.GetStream(key, opts.toKVJS("stream"))
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrNotFound
	}
	return v, nil
}

// GetStrings gets the string values for up to 100 keys in a single call,
// wrapping the L1 KVNamespace.GetTextMultiple binding
// (exp/cloudflare/kv.KVNamespace.GetTextMultiple). Unlike GetString, a
// missing key is simply left out of the result map instead of causing an
// error: there's no single ErrNotFound to return for a call spanning
// several keys, and workers-types' own Map<string, string | null> return
// shape already distinguishes "missing" (nil) per key without erroring the
// whole call.
func (ns *Namespace) GetStrings(keys []string, opts *GetOptions) (map[string]string, error) {
	m, err := ns.instance.GetTextMultiple(keys, opts.toKVJS("text"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if v != nil {
			out[k] = *v
		}
	}
	return out, nil
}
