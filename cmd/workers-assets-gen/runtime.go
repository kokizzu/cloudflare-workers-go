package main

type Runtime string

const (
	RuntimeCloudflare Runtime = "cloudflare"
	RuntimeBrowser    Runtime = "browser"
	RuntimeDeno       Runtime = "deno"
	RuntimeNeon       Runtime = "neon"
)

// fileRule describes one file emitted into the build directory: src is read
// from the runtime's asset directory (assets/runtimes/<name>) and written to
// dest inside the build directory. When projectOverride is non-empty, a file
// of that name in the current directory (the project root) is copied instead
// of the embedded asset when it exists -- e.g. the user's own crons.mjs wins
// over the stub.
type fileRule struct {
	src             string
	dest            string
	projectOverride string
}

// runtimeSpec describes what workers-assets-gen emits for one runtime. The
// runtime's assets live in assets/runtimes/<name>/; files lists them in emit
// order. cfClassFlags marks whether the runtime accepts the Cloudflare-only
// class flags (-durable-objects, -workflows, -entrypoints): those append
// subclasses of Cloudflare-specific base classes (GoDurableObject,
// GoWorkflowEntrypoint, GoWorkerEntrypoint), which exist only in the
// Cloudflare worker, so passing them to another runtime would emit class
// definitions whose base class is undefined.
type runtimeSpec struct {
	// workerFile is the generated worker entry file inside the build
	// directory; generated class subclasses are appended to it.
	workerFile   string
	files        []fileRule
	cfClassFlags bool
}

var runtimeSpecs = map[Runtime]runtimeSpec{
	RuntimeCloudflare: {
		workerFile: "worker.mjs",
		files: []fileRule{
			{src: "runtime.mjs", dest: "runtime.mjs"},
			{src: "worker.mjs", dest: "worker.mjs"},
		},
		cfClassFlags: true,
	},
	RuntimeBrowser: {
		workerFile: "worker.mjs",
		files: []fileRule{
			{src: "runtime.mjs", dest: "runtime.mjs"},
			{src: "worker.mjs", dest: "worker.mjs"},
		},
	},
	RuntimeDeno: {
		workerFile: "worker.mjs",
		files: []fileRule{
			{src: "runtime.mjs", dest: "runtime.mjs"},
			{src: "worker.mjs", dest: "worker.mjs"},
			{src: "main.mjs", dest: "main.mjs"},
			{src: "crons.mjs", dest: "crons.mjs", projectOverride: "crons.mjs"},
		},
	},
	RuntimeNeon: {
		// Neon Functions only loads an entry file named index.mjs or
		// index.js, so the worker entry point is named accordingly.
		// https://neon.com/docs/compute/functions/deploy
		workerFile: "index.mjs",
		files: []fileRule{
			{src: "runtime.mjs", dest: "runtime.mjs"},
			{src: "index.mjs", dest: "index.mjs"},
		},
	},
}

func (r Runtime) IsValid() bool {
	_, ok := runtimeSpecs[r]
	return ok
}

func (r Runtime) spec() runtimeSpec {
	return runtimeSpecs[r]
}
