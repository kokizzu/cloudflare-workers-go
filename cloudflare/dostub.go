package cloudflare

import (
	"fmt"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/exp/cloudflare/durableobjects"
	"github.com/syumai/workers-go/exp/cloudflare/rpc"
	"github.com/syumai/workers-go/internal/jshttp"
	"github.com/syumai/workers-go/internal/jsutil"
)

// DurableObjectNamespace represents the namespace of the durable object.
//
// It is a thin wrapper around exp/cloudflare/durableobjects'
// DurableObjectNamespace/DurableObjectID (the generated L1 bindings); its
// own exported API is unchanged from before that package existed (see
// tmp/06-codegen-spec.md 5.2 item 2).
type DurableObjectNamespace struct {
	ns *durableobjects.DurableObjectNamespace
}

// NewDurableObjectNamespace returns the namespace for the `varName` binding.
//
// This binding must be defined in the `wrangler.toml` file. The method will
// return an `error` when there is no binding defined by `varName`.
func NewDurableObjectNamespace(varName string) (*DurableObjectNamespace, error) {
	ns, err := durableobjects.NewDurableObjectNamespace(varName)
	if err != nil {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &DurableObjectNamespace{ns: ns}, nil
}

// IdFromName returns a `DurableObjectId` for the given `name`.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#deriving-ids-from-names
func (ns *DurableObjectNamespace) IdFromName(name string) *DurableObjectId {
	id, err := ns.ns.IDFromName(name)
	if err != nil {
		// idFromName never throws for a name-derived ID on the JS side; this
		// mirrors the pre-L1 implementation, which called the JS method
		// directly (an uncaught throw there would also have panicked, via
		// syscall/js's own panic-on-exception behavior).
		panic(err)
	}
	return &DurableObjectId{id: id}
}

// Get obtains the durable object stub for `id`.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#obtaining-an-object-stub
func (ns *DurableObjectNamespace) Get(id *DurableObjectId) (*DurableObjectStub, error) {
	if id == nil || id.id == nil {
		return nil, fmt.Errorf("invalid UniqueGlobalId")
	}
	stub, err := ns.ns.Get(id.id, durableobjects.DurableObjectNamespaceGetDurableObjectOptions{})
	if err != nil {
		return nil, err
	}
	return &DurableObjectStub{val: stub}, nil
}

// DurableObjectId represents an identifier for a durable object.
type DurableObjectId struct {
	id *durableobjects.DurableObjectID
}

// DurableObjectStub represents the stub to communicate with the durable object.
type DurableObjectStub struct {
	val js.Value
}

// Fetch calls the durable objects `fetch()` method.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#sending-http-requests
func (s *DurableObjectStub) Fetch(req *http.Request) (*http.Response, error) {
	jsReq := jshttp.ToJSRequest(req)

	promise := s.val.Call("fetch", jsReq)
	jsRes, err := jsutil.AwaitPromise(promise)
	if err != nil {
		return nil, err
	}

	return jshttp.ToResponse(jsRes)
}

// RPC returns an RPC client for calling one of this Durable Object's own
// methods beyond fetch() (see exp/cloudflare/rpc and
// tmp/06-codegen-spec.md 8) -- Durable Objects support Workers RPC the same
// way a WorkerEntrypoint reached over a Service binding does.
func (s *DurableObjectStub) RPC() *rpc.Stub {
	return rpc.StubFromJS(s.val)
}

// FetchWebSocket forwards req -- expected to be a WebSocket upgrade
// request -- to the durable object's `fetch()` trigger, and, if that
// trigger's response is itself a WebSocket upgrade (status 101 with a
// ResponseInit.webSocket set, e.g. by exp/cloudflare/websocket.Upgrade or
// UpgradeHibernating running inside the durable object), attaches that same
// WebSocket to w so the original client's handshake completes.
//
// Unlike Fetch, which reconstructs a *http.Response from the JS Response
// (headers, status, body) and therefore has nowhere to carry the
// runtime-specific webSocket pairing a 101 response needs, FetchWebSocket
// forwards the JS Response's `webSocket` property directly -- the same
// value a plain JS Worker gets for free from `return stub.fetch(request)`.
// w must support attaching a WebSocket the way exp/cloudflare/websocket.Upgrade
// requires of its own http.ResponseWriter argument (true for the
// http.ResponseWriter workers.Serve hands a registered handler).
func (s *DurableObjectStub) FetchWebSocket(w http.ResponseWriter, req *http.Request) error {
	setter, ok := w.(interface{ SetWebSocket(js.Value) })
	if !ok {
		return fmt.Errorf("cloudflare: FetchWebSocket: %T does not support attaching a WebSocket to its response", w)
	}
	jsReq := jshttp.ToJSRequest(req)

	promise := s.val.Call("fetch", jsReq)
	jsRes, err := jsutil.AwaitPromise(promise)
	if err != nil {
		return err
	}

	ws := jsRes.Get("webSocket")
	if ws.IsUndefined() || ws.IsNull() {
		return fmt.Errorf("cloudflare: FetchWebSocket: durable object's fetch() response has no webSocket (status %d)", jsRes.Get("status").Int())
	}
	setter.SetWebSocket(ws)
	return nil
}
