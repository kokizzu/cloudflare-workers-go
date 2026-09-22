package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenericFuncForwarding drives collectPackages and writePackage against
// testdata/genericfunc -- a small fixture module whose generic functions
// exercise every shape renderGenericFuncWrapper must support -- and checks
// both the generated wrapper source and that it actually compiles against
// the fixture module. This guards the fix for the "genforward does not
// support forwarding generic functions" failure genforward used to raise
// for exp/cloudflare/rpc.MethodJSON and exp/cloudflare/workflows.DoJSON /
// DoJSONWithConfig.
func TestGenericFuncForwarding(t *testing.T) {
	fixtureDir, err := filepath.Abs("testdata/genericfunc")
	if err != nil {
		t.Fatalf("resolving fixture dir: %v", err)
	}
	fixtureMod, err := readModule(fixtureDir)
	if err != nil {
		t.Fatalf("reading fixture go.mod: %v", err)
	}

	pkgs, err := collectPackages(fixtureDir, fixtureMod.path)
	if err != nil {
		t.Fatalf("collectPackages: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("collectPackages returned %d packages, want 1: %+v", len(pkgs), pkgs)
	}
	p := pkgs[0]
	if !p.hostOK {
		t.Fatalf("fixture package unexpectedly not hostOK")
	}

	wantGeneric := map[string]bool{"MethodLike": true, "DoLike": true, "Sum": true, "Noop": true}
	gotGeneric := map[string]bool{}
	for _, s := range p.common {
		if s.kind == kindFunc && s.genFunc != nil {
			gotGeneric[s.name] = true
		}
	}
	for name := range wantGeneric {
		if !gotGeneric[name] {
			t.Errorf("symbol %s: not classified as a generic func", name)
		}
	}

	outDir := t.TempDir()
	if err := writePackage(outDir, p); err != nil {
		t.Fatalf("writePackage: %v", err)
	}

	forwardGo, err := os.ReadFile(filepath.Join(outDir, "forward.go"))
	if err != nil {
		t.Fatalf("reading generated forward.go: %v", err)
	}
	got := string(forwardGo)

	// Each wrapper must reproduce the source signature (qualifying both
	// same-package and cross-package types via the src import alias, or the
	// real import alias respectively) and forward to src.<Name> with
	// explicit type arguments.
	wantSnippets := []string{
		"func MethodLike[Out any](fn func(ctx context.Context, args []json.RawMessage) (Out, error)) src.Box {\n\treturn src.MethodLike[Out](fn)\n}",
		"func DoLike[T any](b *src.Box, name string, fn func(ctx context.Context) (T, error)) (T, error) {\n\treturn src.DoLike[T](b, name, fn)\n}",
		// Sum's blank first parameter must be renamed (a blank identifier
		// can't be used as a value in the forwarding call), and its
		// variadic tail must forward with "...".
		"func Sum[T ~int | ~float64, N any](arg0 N, nums ...T) T {\n\treturn src.Sum[T, N](arg0, nums...)\n}",
		"func Noop[T any](v T) {\n\tsrc.Noop[T](v)\n}",
	}
	for _, snippet := range wantSnippets {
		if !strings.Contains(got, snippet) {
			t.Errorf("generated forward.go missing expected snippet:\n%s\n\ngot:\n%s", snippet, got)
		}
	}

	// The wrapper signatures pull in context and encoding/json beyond the
	// src import; both must be imported under their real package names.
	for _, imp := range []string{`context "context"`, `json "encoding/json"`} {
		if !strings.Contains(got, imp) {
			t.Errorf("generated forward.go missing import %q\n\ngot:\n%s", imp, got)
		}
	}

	// Finally, prove the generated package actually compiles against the
	// fixture module: give outDir its own go.mod requiring the fixture
	// module via a replace directive (mirroring how
	// internal/cmd/genforward/mirror-tests/run.sh wires up its fixtures),
	// then build it.
	goModContent := "module example.com/genforward-test/genericfunc-forward\n\n" +
		"go 1.21\n\n" +
		"require " + fixtureMod.path + " v0.0.0\n\n" +
		"replace " + fixtureMod.path + " => " + fixtureDir + "\n"
	if err := os.WriteFile(filepath.Join(outDir, "go.mod"), []byte(goModContent), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}

	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = outDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... in generated package failed: %v\n%s", err, out)
	}

	cmd = exec.Command("go", "vet", "./...")
	cmd.Dir = outDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet ./... in generated package failed: %v\n%s", err, out)
	}
}
