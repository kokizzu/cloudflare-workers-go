# workflow-go

An example of hosting a Go type's `run()` as a
[Workflow](https://developers.cloudflare.com/workflows/)'s
`WorkflowEntrypoint`, using
[`exp/cloudflare/workflows`](https://github.com/syumai/workers-go/tree/main/exp/cloudflare/workflows).

`runMyWorkflow` (`main.go`) is a `workflows.Runner` registered for the
`MyWorkflow` class: it runs a step that returns a JSON result, sleeps for a
second, then runs a second step that reuses the first step's result. The
Worker's regular `fetch` handler (`handleIndex`) creates and inspects
instances of that same Workflow through the client-side L1
(`workflows.NewWorkflow("MY_WORKFLOW")`), the same way any Worker talks to a
Workflow, whether or not it hosts that class itself.

## Running

### Requirements

This project requires these tools to be installed globally.

* wrangler
* Go 1.24.0 or later

### Supported commands

```
make dev     # run dev server
make build   # build Go Wasm binary
make deploy  # deploy worker
```

`make build` passes `-workflows=MyWorkflow` to `workers-assets-gen`, which
appends the class definition `worker.mjs` needs
(`export class MyWorkflow extends GoWorkflowEntrypoint { ... }`) — see
`cmd/workers-assets-gen/README.md`.

### Trying it out

1. Start the dev server.
```sh
wrangler dev
```

2. Create a new `MyWorkflow` instance:
```sh
curl -X POST http://localhost:8787/
# {"id":"<instance-id>"}
```

3. Poll its status until it's `"complete"` (step 1 finishes immediately,
   then the Workflow sleeps for a second before step 2 runs):
```sh
curl "http://localhost:8787/?id=<instance-id>"
# {"status":"complete","output":{"step1":{"message":"hello from step 1"},"step2":{"combined":"hello from step 1 + step 2"}}}
```
