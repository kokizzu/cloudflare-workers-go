//go:build js && wasm

package dispatch

import (
	"github.com/syumai/workers-go/cloudflare/fetch"
)

// GetClient looks up the Worker script named name in this dispatch
// namespace (Get) and wraps the returned Fetcher's js.Value binding as a
// *fetch.Client (fetch.NewClient(fetch.WithBinding(v))), the same way
// exp/cloudflare/hyperdrive.Hyperdrive.Connect bridges a synchronous JS
// return value into another package's Go type — Get itself only returns
// js.Value since Fetcher has no generated Go type of its own (see
// dispatch.yaml's doc comment). opts may be nil.
func (x *DispatchNamespace) GetClient(name string, opts *DynamicDispatchOptions) (*fetch.Client, error) {
	var options DynamicDispatchOptions
	if opts != nil {
		options = *opts
	}
	v, err := x.Get(name, nil, options)
	if err != nil {
		return nil, err
	}
	return fetch.NewClient(fetch.WithBinding(v)), nil
}
