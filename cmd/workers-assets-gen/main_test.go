package main

import (
	"bytes"
	"flag"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// update regenerates the golden files under testdata/golden when set.
// Run: go test ./cmd/workers-assets-gen/... -run TestRunMain_fileList -update
var update = flag.Bool("update", false, "update golden files")

func TestParseClassNames(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "empty", in: "", want: nil},
		{name: "single", in: "Counter", want: []string{"Counter"}},
		{name: "multiple, stray whitespace/commas", in: "Counter, ,Room", want: []string{"Counter", "Room"}},
		{name: "duplicate", in: "Counter,Counter", wantErr: true},
		{name: "duplicate after trimming", in: "Counter, Counter ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseClassNames(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseClassNames(%q) = %v, <nil>, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseClassNames(%q) failed: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseClassNames(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRunMain(t *testing.T) {
	tests := map[string]struct {
		mode          Mode
		runtime       Runtime
		wantWasmExecF string
	}{
		"go-cloudflare": {
			mode: ModeGo, runtime: RuntimeCloudflare,
			wantWasmExecF: "wasm_exec_go.js",
		},
		"go-browser": {
			mode: ModeGo, runtime: RuntimeBrowser,
			wantWasmExecF: "wasm_exec_go.js",
		},
		"go-neon": {
			mode: ModeGo, runtime: RuntimeNeon,
			wantWasmExecF: "wasm_exec_go.js",
		},
		"tinygo-cloudflare": {
			mode: ModeTinygo, runtime: RuntimeCloudflare,
			wantWasmExecF: "wasm_exec_tinygo.js",
		},
		"tinygo-browser": {
			mode: ModeTinygo, runtime: RuntimeBrowser,
			wantWasmExecF: "wasm_exec_tinygo.js",
		},
		"tinygo-neon": {
			mode: ModeTinygo, runtime: RuntimeNeon,
			wantWasmExecF: "wasm_exec_tinygo.js",
		},
		"go-deno": {
			mode: ModeGo, runtime: RuntimeDeno,
			wantWasmExecF: "wasm_exec_go.js",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := runMain(tt.mode, tt.runtime, dir, nil, nil, nil); err != nil {
				t.Fatalf("runMain() error = %v", err)
			}

			wantWasmExec, err := assets.ReadFile(path.Join(coreDirPath, tt.wantWasmExecF))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, "wasm_exec.js"), wantWasmExec)

			wantCore, err := assets.ReadFile(path.Join(coreDirPath, "core.mjs"))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, "core.mjs"), wantCore)

			spec := tt.runtime.spec()
			runtimeDir := path.Join(runtimesDirPath, string(tt.runtime))
			wantRuntime, err := assets.ReadFile(path.Join(runtimeDir, "runtime.mjs"))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, "runtime.mjs"), wantRuntime)

			wantWorker, err := assets.ReadFile(path.Join(runtimeDir, spec.workerFile))
			if err != nil {
				t.Fatalf("assets.ReadFile() error = %v", err)
			}
			assertFileEqualsBytes(t, filepath.Join(dir, spec.workerFile), wantWorker)
		})
	}
}

// TestRunMain_cleansOutputDir pins the current (dangerous) behavior of
// runMain: it removes the entire output directory (os.RemoveAll) before
// writing to it, so any pre-existing files there -- including files
// unrelated to workers-assets-gen -- are deleted. This is documented here
// so that a future change to this behavior is a deliberate decision, not
// an accident.
func TestRunMain_cleansOutputDir(t *testing.T) {
	dir := t.TempDir()
	staleFile := filepath.Join(dir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	if err := runMain(ModeGo, RuntimeCloudflare, dir, nil, nil, nil); err != nil {
		t.Fatalf("runMain() error = %v", err)
	}

	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Errorf("stale file %q survived runMain(); os.Stat error = %v, want os.IsNotExist", staleFile, err)
	}
}

func TestRunMain_invalidMode(t *testing.T) {
	dir := t.TempDir()
	if err := runMain(Mode("invalid"), RuntimeCloudflare, dir, nil, nil, nil); err == nil {
		t.Error("runMain() error = nil, want non-nil for an invalid mode")
	}
}

func TestRunMain_invalidRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := runMain(ModeGo, Runtime("invalid"), dir, nil, nil, nil); err == nil {
		t.Error("runMain() error = nil, want non-nil for an invalid runtime")
	}
}

// TestRunMain_cfClassFlagsRequireCloudflare ensures the Cloudflare-only
// class flags are rejected for other runtimes: they append subclasses of
// base classes (GoDurableObject, GoWorkflowEntrypoint, GoWorkerEntrypoint)
// that only exist in the Cloudflare worker, so generating them elsewhere
// would emit a worker whose classes extend nothing.
func TestRunMain_cfClassFlagsRequireCloudflare(t *testing.T) {
	tests := map[string]struct {
		durableObjects []string
		workflows      []string
		entrypoints    []entrypointSpec
	}{
		"durable-objects": {durableObjects: []string{"Counter"}},
		"workflows":       {workflows: []string{"MyWorkflow"}},
		"entrypoints":     {entrypoints: []entrypointSpec{{Name: "MyService"}}},
	}
	for _, runtime := range []Runtime{RuntimeBrowser, RuntimeDeno, RuntimeNeon} {
		for name, tt := range tests {
			t.Run(string(runtime)+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				err := runMain(ModeGo, runtime, dir, tt.durableObjects, tt.workflows, tt.entrypoints)
				if err == nil {
					t.Fatalf("runMain() error = nil, want non-nil")
				}
				if !strings.Contains(err.Error(), "-runtime=cloudflare") {
					t.Errorf("runMain() error = %q, want it to mention -runtime=cloudflare", err)
				}
				// The error must be raised before the build directory is
				// populated, or stale output would silently survive.
				entries, readErr := os.ReadDir(dir)
				if readErr != nil {
					t.Fatalf("os.ReadDir() error = %v", readErr)
				}
				if len(entries) != 0 {
					t.Errorf("build dir is not empty after rejected run: %v", entries)
				}
			})
		}
	}

	// Sanity check: the same flags are accepted for Cloudflare.
	dir := t.TempDir()
	if err := runMain(ModeGo, RuntimeCloudflare, dir, []string{"Counter"}, []string{"MyWorkflow"}, []entrypointSpec{{Name: "MyService"}}); err != nil {
		t.Fatalf("runMain() error = %v", err)
	}
}

func TestRunMain_fileList(t *testing.T) {
	tests := map[string]struct {
		mode    Mode
		runtime Runtime
	}{
		"go-cloudflare":     {mode: ModeGo, runtime: RuntimeCloudflare},
		"go-browser":        {mode: ModeGo, runtime: RuntimeBrowser},
		"go-neon":           {mode: ModeGo, runtime: RuntimeNeon},
		"tinygo-cloudflare": {mode: ModeTinygo, runtime: RuntimeCloudflare},
		"tinygo-browser":    {mode: ModeTinygo, runtime: RuntimeBrowser},
		"tinygo-neon":       {mode: ModeTinygo, runtime: RuntimeNeon},
		"go-deno":           {mode: ModeGo, runtime: RuntimeDeno},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := runMain(tt.mode, tt.runtime, dir, nil, nil, nil); err != nil {
				t.Fatalf("runMain() error = %v", err)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			sort.Strings(names)
			got := strings.Join(names, "\n") + "\n"

			goldenPath := filepath.Join("testdata", "golden", name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(goldenPath), os.ModePerm); err != nil {
					t.Fatalf("os.MkdirAll() error = %v", err)
				}
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("os.ReadFile(%q) error = %v (run with -update to (re)generate)", goldenPath, err)
			}
			if got != string(want) {
				t.Errorf("file list for %s mismatches golden %q\ngot:\n%s\nwant:\n%s", name, goldenPath, got, want)
			}
		})
	}
}

// TestRunMain_denoCrons covers the Deno cron support: main.mjs always
// imports ./crons.mjs, and the output crons.mjs is the project's own
// ./crons.mjs when present, else the stub asset.
func TestRunMain_denoCrons(t *testing.T) {
	t.Run("stub when no project crons.mjs", func(t *testing.T) {
		chdir(t, t.TempDir())
		dir := t.TempDir()
		if err := runMain(ModeGo, RuntimeDeno, dir, nil, nil, nil); err != nil {
			t.Fatalf("runMain() error = %v", err)
		}
		wantCrons, err := assets.ReadFile(path.Join(runtimesDirPath, string(RuntimeDeno), "crons.mjs"))
		if err != nil {
			t.Fatalf("assets.ReadFile() error = %v", err)
		}
		assertFileEqualsBytes(t, filepath.Join(dir, "crons.mjs"), wantCrons)
		mainContent, err := os.ReadFile(filepath.Join(dir, "main.mjs"))
		if err != nil {
			t.Fatalf("os.ReadFile() error = %v", err)
		}
		if !strings.Contains(string(mainContent), `import "./crons.mjs";`) {
			t.Errorf("main.mjs does not import ./crons.mjs:\n%s", mainContent)
		}
	})

	t.Run("project crons.mjs wins over stub", func(t *testing.T) {
		projectDir := t.TempDir()
		want := []byte("// user cron definitions\n")
		if err := os.WriteFile(filepath.Join(projectDir, "crons.mjs"), want, 0o644); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}
		chdir(t, projectDir)
		dir := t.TempDir()
		if err := runMain(ModeGo, RuntimeDeno, dir, nil, nil, nil); err != nil {
			t.Fatalf("runMain() error = %v", err)
		}
		assertFileEqualsBytes(t, filepath.Join(dir, "crons.mjs"), want)
	})

	t.Run("no crons.mjs for other runtimes", func(t *testing.T) {
		chdir(t, t.TempDir())
		dir := t.TempDir()
		if err := runMain(ModeGo, RuntimeCloudflare, dir, nil, nil, nil); err != nil {
			t.Fatalf("runMain() error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "crons.mjs")); !os.IsNotExist(err) {
			t.Errorf("crons.mjs exists in cloudflare output; os.Stat error = %v, want os.IsNotExist", err)
		}
	})
}

func TestParseEntrypoints(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []entrypointSpec
		wantErr bool
	}{
		{name: "empty", in: "", want: nil},
		{
			name: "name only, no methods",
			in:   "Other",
			want: []entrypointSpec{{Name: "Other", Methods: nil}},
		},
		{
			name: "name with methods, mixing ; and , as intended",
			in:   "MyService:add,greet;Other",
			want: []entrypointSpec{
				{Name: "MyService", Methods: []string{"add", "greet"}},
				{Name: "Other", Methods: nil},
			},
		},
		{
			name: "stray whitespace and empty method dropped",
			in:   " MyService : add , ,greet ",
			want: []entrypointSpec{{Name: "MyService", Methods: []string{"add", "greet"}}},
		},
		{name: "empty class name", in: ":add", wantErr: true},
		{name: "duplicate class name", in: "MyService:add;MyService:greet", wantErr: true},
		{name: "duplicate method name within a spec", in: "MyService:add,add", wantErr: true},
		{name: "explicit fetch method name", in: "MyService:fetch", wantErr: true},
		{name: "explicit fetch method name among others", in: "MyService:add,fetch", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEntrypoints(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseEntrypoints(%q) = %+v, <nil>, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEntrypoints(%q) failed: %v", tt.in, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseEntrypoints(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateClassNames(t *testing.T) {
	tests := []struct {
		name           string
		durableObjects []string
		workflows      []string
		entrypoints    []entrypointSpec
		wantErr        bool
	}{
		{
			name:           "disjoint names ok",
			durableObjects: []string{"Counter"},
			workflows:      []string{"MyWorkflow"},
			entrypoints:    []entrypointSpec{{Name: "MyService"}},
		},
		{
			name:           "durable object and workflow share a name",
			durableObjects: []string{"Foo"},
			workflows:      []string{"Foo"},
			wantErr:        true,
		},
		{
			name:        "workflow and entrypoint share a name",
			workflows:   []string{"Foo"},
			entrypoints: []entrypointSpec{{Name: "Foo"}},
			wantErr:     true,
		},
		{
			name:           "durable object and entrypoint share a name",
			durableObjects: []string{"Foo"},
			entrypoints:    []entrypointSpec{{Name: "Foo"}},
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClassNames(tt.durableObjects, tt.workflows, tt.entrypoints)
			if tt.wantErr && err == nil {
				t.Fatal("validateClassNames() = <nil>, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateClassNames() failed: %v", err)
			}
		})
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) error = %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func assertFileEqualsBytes(t *testing.T, filePath string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v", filePath, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("file %q content does not match expected asset content", filePath)
	}
}
