// Command oldpath is a smoke test for the forwarding mirror described in
// https://github.com/syumai/workers/issues/173: a project that imports only
// the mirror module (the old github.com/syumai/workers import path, and
// nothing from the new module directly) must keep building unchanged.
//
// internal/cmd/genforward/mirror-tests/run.sh builds this module against a locally generated mirror
// via a "replace github.com/syumai/workers => <generated mirror>" directive,
// so the import path below is the real mirror module path.
package main

import (
	"context"
	"encoding/json"
	"net/http"

	workers "github.com/syumai/workers"
	"github.com/syumai/workers/cloudflare/cron"
	"github.com/syumai/workers/cloudflare/fetch"
	"github.com/syumai/workers/cloudflare/kv"
	"github.com/syumai/workers/cloudflare/queues"
	"github.com/syumai/workers/cloudflare/r2"
	"github.com/syumai/workers/exp/cloudflare/rpc"
)

func main() {
	var mux http.ServeMux
	workers.ServeNonBlock(&mux)

	ns, err := kv.NewNamespace("MY_KV")
	if err == nil {
		_, _ = ns.GetString("key", nil)
	}

	bucket, err := r2.NewBucket("MY_BUCKET")
	if err == nil {
		_, _ = bucket.Get("key")
	}

	client := fetch.NewClient()
	_ = client

	producer, err := queues.NewProducer("MY_QUEUE")
	if err == nil {
		_ = producer
	}

	cron.ScheduleTaskNonBlock(func(ctx context.Context) error {
		return nil
	})

	// rpc.MethodJSON is a generic function; this proves genforward's
	// type-parameterized wrapper (see internal/cmd/genforward/gen.go's
	// renderGenericFuncWrapper) type-checks and compiles through the mirror
	// module, not just `go vet`-clean in isolation.
	greet := rpc.MethodJSON(func(ctx context.Context, args []json.RawMessage) (string, error) {
		return "hello", nil
	})
	rpc.Register("Greeter", map[string]rpc.Method{"greet": greet})

	workers.Ready()
}
