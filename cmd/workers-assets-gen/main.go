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
	)
	flag.StringVar(&mode, "mode", string(ModeTinygo), `build mode: tinygo or go`)
	flag.StringVar(&runtime, "runtime", string(RuntimeCloudflare), `runtime: cloudflare`)
	flag.StringVar(&buildDirPath, "o", defaultBuildDirPath, `output dir path: defaults to "build"`)
	flag.StringVar(&durableObjects, "durable-objects", "", `comma-separated list of Durable Object class names to define in worker.mjs (e.g. "Counter,Room")`)
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
	if err := runMain(Mode(mode), Runtime(runtime), buildDirPath, parseDurableObjects(durableObjects)); err != nil {
		fmt.Fprintf(os.Stderr, "err: %v", err)
		os.Exit(1)
	}
}

// parseDurableObjects splits a comma-separated -durable-objects flag value
// into class names, dropping empty entries (so "" produces nil, and stray
// whitespace/commas like "Counter, ,Room" don't produce blank class names).
func parseDurableObjects(s string) []string {
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

func runMain(mode Mode, runtime Runtime, buildDirPath string, durableObjects []string) error {
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
	if err := copyCommonAssets(buildDirPath); err != nil {
		return err
	}
	if err := appendDurableObjectClasses(buildDirPath, durableObjects); err != nil {
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

func copyCommonAssets(buildDirPath string) error {
	entries, err := assets.ReadDir(commonDirPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		destPath := path.Join(buildDirPath, entry.Name())
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
