package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
)

//go:embed assets
var assets embed.FS

const (
	assetDirPath        = "assets"
	commonDirPath       = "assets/common"
	runtimeDirPath      = "assets/runtime"
	entryDirPath        = "assets/entry"
	defaultBuildDirPath = "build"
)

func main() {
	var (
		mode         string
		runtime      string
		buildDirPath string
	)
	flag.StringVar(&mode, "mode", string(ModeTinygo), `build mode: tinygo or go`)
	flag.StringVar(&runtime, "runtime", string(RuntimeCloudflare), `runtime: cloudflare, browser, or deno`)
	flag.StringVar(&buildDirPath, "o", defaultBuildDirPath, `output dir path: defaults to "build"`)
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
	if err := runMain(Mode(mode), Runtime(runtime), buildDirPath); err != nil {
		fmt.Fprintf(os.Stderr, "err: %v", err)
		os.Exit(1)
	}
}

func runMain(mode Mode, runtime Runtime, buildDirPath string) error {
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
	if err := copyEntryAsset(runtime, buildDirPath); err != nil {
		return err
	}
	if err := copyCommonAssets(buildDirPath); err != nil {
		return err
	}
	return nil
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

// copyEntryAsset copies the runtime's entry point file to "main.ts" in the
// build directory when the runtime provides one (e.g. Deno, whose entry point
// calls Deno.serve). Runtimes that use worker.mjs as their entry point
// (Cloudflare, browser) have no entry asset and are skipped.
func copyEntryAsset(runtime Runtime, buildDirPath string) error {
	originPath := path.Join(entryDirPath, string(runtime)+".ts")
	if _, err := assets.ReadFile(originPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	destPath := path.Join(buildDirPath, "main.ts")
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
