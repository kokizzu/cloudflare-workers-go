package main

import (
	"path"
	"testing"
)

func TestRuntime_IsValid(t *testing.T) {
	tests := map[string]struct {
		runtime Runtime
		want    bool
	}{
		"cloudflare": {runtime: RuntimeCloudflare, want: true},
		"browser":    {runtime: RuntimeBrowser, want: true},
		"deno":       {runtime: RuntimeDeno, want: true},
		"neon":       {runtime: RuntimeNeon, want: true},
		"empty":      {runtime: Runtime(""), want: false},
		"invalid":    {runtime: Runtime("invalid"), want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.runtime.IsValid()
			if got != tt.want {
				t.Errorf("Runtime(%q).IsValid() = %v, want %v", tt.runtime, got, tt.want)
			}
		})
	}
}

func TestRuntimeSpec_workerFile(t *testing.T) {
	tests := map[string]struct {
		runtime Runtime
		want    string
	}{
		"cloudflare": {runtime: RuntimeCloudflare, want: "worker.mjs"},
		"browser":    {runtime: RuntimeBrowser, want: "worker.mjs"},
		"deno":       {runtime: RuntimeDeno, want: "worker.mjs"},
		"neon":       {runtime: RuntimeNeon, want: "index.mjs"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.runtime.spec().workerFile
			if got != tt.want {
				t.Errorf("Runtime(%q).spec().workerFile = %q, want %q", tt.runtime, got, tt.want)
			}
		})
	}
}

// TestRuntimeSpec_assetsExist pins the invariant that every fileRule in a
// runtime spec resolves to an embedded asset in assets/runtimes/<name>/.
func TestRuntimeSpec_assetsExist(t *testing.T) {
	for runtime, spec := range runtimeSpecs {
		runtimeDir := path.Join(runtimesDirPath, string(runtime))
		for _, rule := range spec.files {
			if _, err := assets.ReadFile(path.Join(runtimeDir, rule.src)); err != nil {
				t.Errorf("%s: fileRule src %q: %v", runtime, rule.src, err)
			}
		}
	}
}
