package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

//go:embed assets
var assets embed.FS

const (
	assetDirPath        = "assets"
	commonDirPath       = "assets/common"
	runtimeDirPath      = "assets/runtime"
	defaultBuildDirPath = "build"
)

func main() {
	var (
		mode           string
		runtime        string
		buildDirPath   string
		durableObjects string
		workflows      string
		entrypoints    string
	)
	flag.StringVar(&mode, "mode", string(ModeTinygo), `build mode: tinygo or go`)
	flag.StringVar(&runtime, "runtime", string(RuntimeCloudflare), `runtime: cloudflare, browser, or neon`)
	flag.StringVar(&buildDirPath, "o", defaultBuildDirPath, `output dir path: defaults to "build"`)
	flag.StringVar(&durableObjects, "durable-objects", "", `comma-separated list of Durable Object class names to define in worker.mjs (e.g. "Counter,Room")`)
	flag.StringVar(&workflows, "workflows", "", `comma-separated list of Workflow class names to define in worker.mjs (e.g. "MyWorkflow,Other")`)
	flag.StringVar(&entrypoints, "entrypoints", "", `semicolon-separated list of WorkerEntrypoint (RPC) specs to define in worker.mjs, each "Name" or "Name:method1,method2,..." (e.g. "MyService:add,greet;Other")`)
	flag.Parse()
	if !Mode(mode).IsValid() {
		flag.PrintDefaults()
		os.Exit(1)
		return
	}
	if !Runtime(runtime).IsValid() {
		flag.PrintDefaults()
		os.Exit(1)
		return
	}
	entrypointSpecs, err := parseEntrypoints(entrypoints)
	if err != nil {
		fmt.Fprintf(os.Stderr, "err: %v", err)
		os.Exit(1)
		return
	}
	if err := runMain(Mode(mode), Runtime(runtime), buildDirPath, parseClassNames(durableObjects), parseClassNames(workflows), entrypointSpecs); err != nil {
		fmt.Fprintf(os.Stderr, "err: %v", err)
		os.Exit(1)
	}
}

// parseClassNames splits a comma-separated flag value (-durable-objects or
// -workflows) into class names, dropping empty entries (so "" produces nil,
// and stray whitespace/commas like "Counter, ,Room" don't produce blank
// class names).
func parseClassNames(s string) []string {
	var names []string
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// entrypointSpec is one parsed -entrypoints item: a WorkerEntrypoint class
// Name plus its RPC Methods (possibly empty -- a Name-only spec is valid;
// see parseEntrypoints).
type entrypointSpec struct {
	Name    string
	Methods []string
}

// parseEntrypoints splits the -entrypoints flag value into entrypointSpecs.
// The flag is a semicolon-separated list of specs, each either "Name" (no
// RPC methods -- just the always-generated fetch()) or
// "Name:method1,method2,..." (comma-separated method names). Stray
// whitespace and empty items/methods (e.g. "MyService: ,greet") are
// dropped. "" produces nil.
func parseEntrypoints(s string) ([]entrypointSpec, error) {
	var specs []entrypointSpec
	for _, item := range strings.Split(s, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		name, methodsPart, _ := strings.Cut(item, ":")
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("-entrypoints: empty class name in %q", item)
		}
		var methods []string
		for _, m := range strings.Split(methodsPart, ",") {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			methods = append(methods, m)
		}
		specs = append(specs, entrypointSpec{Name: name, Methods: methods})
	}
	return specs, nil
}

func runMain(mode Mode, runtime Runtime, buildDirPath string, durableObjects, workflows []string, entrypoints []entrypointSpec) error {
	if err := os.RemoveAll(buildDirPath); err != nil {
		return err
	}
	if err := os.MkdirAll(buildDirPath, os.ModePerm); err != nil {
		return err
	}
	if err := copyWasmExecJS(mode, buildDirPath); err != nil {
		return err
	}
	if err := copyRuntimeAssets(runtime, buildDirPath); err != nil {
		return err
	}
	if err := copyCommonAssets(runtime, buildDirPath); err != nil {
		return err
	}
	if err := appendDurableObjectClasses(buildDirPath, durableObjects); err != nil {
		return err
	}
	if err := appendWorkflowClasses(buildDirPath, workflows); err != nil {
		return err
	}
	if err := appendEntrypointClasses(buildDirPath, entrypoints); err != nil {
		return err
	}
	return nil
}

// appendDurableObjectClasses appends one GoDurableObject subclass
// definition per name in durableObjects to the generated worker.mjs, e.g.
// for "Counter":
//
//	export class Counter extends GoDurableObject { static goClassName = "Counter"; }
//
// wrangler.toml's [[durable_objects.bindings]] class_name (and the class
// this worker.mjs exports for the Durable Object namespace's `new_classes`/
// `new_sqlite_classes` migration) must match one of these names exactly.
// If durableObjects is empty, worker.mjs is left untouched.
func appendDurableObjectClasses(buildDirPath string, durableObjects []string) error {
	if len(durableObjects) == 0 {
		return nil
	}
	workerPath := path.Join(buildDirPath, "worker.mjs")
	f, err := os.OpenFile(workerPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var b strings.Builder
	for _, name := range durableObjects {
		fmt.Fprintf(&b, "\nexport class %s extends GoDurableObject { static goClassName = %q; }\n", name, name)
	}
	_, err = f.WriteString(b.String())
	return err
}

// appendWorkflowClasses appends one GoWorkflowEntrypoint subclass
// definition per name in workflows to the generated worker.mjs, e.g. for
// "MyWorkflow":
//
//	export class MyWorkflow extends GoWorkflowEntrypoint { static goClassName = "MyWorkflow"; }
//
// wrangler.toml's [[workflows]] class_name must match one of these names
// exactly. If workflows is empty, worker.mjs is left untouched.
func appendWorkflowClasses(buildDirPath string, workflows []string) error {
	if len(workflows) == 0 {
		return nil
	}
	workerPath := path.Join(buildDirPath, "worker.mjs")
	f, err := os.OpenFile(workerPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var b strings.Builder
	for _, name := range workflows {
		fmt.Fprintf(&b, "\nexport class %s extends GoWorkflowEntrypoint { static goClassName = %q; }\n", name, name)
	}
	_, err = f.WriteString(b.String())
	return err
}

// appendEntrypointClasses appends one GoWorkerEntrypoint subclass
// definition per entrypointSpec in entrypoints to the generated worker.mjs,
// e.g. for {Name: "MyService", Methods: ["add", "greet"]}:
//
//	export class MyService extends GoWorkerEntrypoint {
//	  static goClassName = "MyService";
//	  async add(...args) { return (await this._bind()).handleRPC("add", args); }
//	  async greet(...args) { return (await this._bind()).handleRPC("greet", args); }
//	  async fetch(req) { return (await this._bind()).handleEntrypointFetch(req); }
//	}
//
// fetch() is always generated, even for a spec with no Methods, since
// GoWorkerEntrypoint's fetch trigger is independent of any RPC methods.
// wrangler.toml's [[services]] entrypoint (or a Durable Object class
// exposing RPC) must match one of these names exactly, as must the
// className passed to rpc.Register/rpc.RegisterFetch on the Go side. If
// entrypoints is empty, worker.mjs is left untouched.
func appendEntrypointClasses(buildDirPath string, entrypoints []entrypointSpec) error {
	if len(entrypoints) == 0 {
		return nil
	}
	workerPath := path.Join(buildDirPath, "worker.mjs")
	f, err := os.OpenFile(workerPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	var b strings.Builder
	for _, ep := range entrypoints {
		fmt.Fprintf(&b, "\nexport class %s extends GoWorkerEntrypoint {\n  static goClassName = %q;\n", ep.Name, ep.Name)
		for _, method := range ep.Methods {
			fmt.Fprintf(&b, "  async %s(...args) { return (await this._bind()).handleRPC(%q, args); }\n", method, method)
		}
		fmt.Fprintf(&b, "  async fetch(req) { return (await this._bind()).handleEntrypointFetch(req); }\n}\n")
	}
	_, err = f.WriteString(b.String())
	return err
}

func copyWasmExecJS(mode Mode, buildDirPath string) error {
	var fileName string
	switch mode {
	case ModeTinygo:
		fileName = "wasm_exec_tinygo.js"
	case ModeGo:
		fileName = "wasm_exec_go.js"
	default:
		return fmt.Errorf("unexpected mode: %s", mode)
	}
	destPath := path.Join(buildDirPath, "wasm_exec.js")
	originPath := path.Join(assetDirPath, fileName)
	if err := copyFile(destPath, originPath); err != nil {
		return err
	}
	return nil
}

func copyRuntimeAssets(runtime Runtime, buildDirPath string) error {
	destPath := path.Join(buildDirPath, "runtime.mjs")
	originPath := path.Join(runtimeDirPath, runtime.AssetFileName())
	if err := copyFile(destPath, originPath); err != nil {
		return err
	}
	return nil
}

func copyCommonAssets(runtime Runtime, buildDirPath string) error {
	entries, err := assets.ReadDir(commonDirPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fileName := entry.Name()
		// Neon Functions only loads an entry file named index.mjs or index.js,
		// so the worker entry point is renamed for that runtime.
		// https://neon.com/docs/compute/functions/deploy
		if runtime == RuntimeNeon && fileName == "worker.mjs" {
			fileName = "index.mjs"
		}
		destPath := path.Join(buildDirPath, fileName)
		originPath := path.Join(commonDirPath, entry.Name())
		if err := copyFile(destPath, originPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(destPath, originPath string) error {
	f, err := assets.ReadFile(originPath)
	if err != nil {
		return err
	}
	dest, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dest.Close()
	_, err = io.Copy(dest, bytes.NewReader(f))
	if err != nil {
		return err
	}
	return nil
}
