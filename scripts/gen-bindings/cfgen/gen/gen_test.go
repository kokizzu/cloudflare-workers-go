package gen

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syumai/workers-go/scripts/gen-bindings/cfgen/ir"
)

var update = flag.Bool("update", false, "update golden files")

func loadFixtureIR(t *testing.T, path string) *ir.IR {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc ir.IR
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc
}

// TestGenerateGolden exercises the full generation pipeline (classification,
// type mapping, naming, and formatting) against a small hand-written IR
// fixture covering: a handle type, a data type, a string-literal alias
// enum, a Promise-returning method, a Record-returning method, an array
// field, an optional field, a rename override, a types override, and a
// handwritten method.
func TestGenerateGolden(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	goldenPath := filepath.Join("testdata", "fixture.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

// TestValidateRejectsUnknownNames ensures cfgen fails loudly (rather than
// silently ignoring) when an overrides file references a declaration or
// member that does not exist, per spec 1.3 "YAML に存在しない宣言名・
// メンバー名を書いたら cfgen はエラーで止まる".
func TestValidateRejectsUnknownNames(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))

	cases := []struct {
		name string
		ov   Overrides
	}{
		{"unknown include", Overrides{Package: "x", Include: []string{"DoesNotExist"}}},
		{"unknown rename target", Overrides{Package: "x", Include: []string{"Widget"}, Rename: map[string]string{"Widget.nope": "Nope"}}},
		{"unknown types target", Overrides{Package: "x", Include: []string{"WidgetInfo"}, Types: map[string]string{"WidgetInfo.nope": "string"}}},
		{"unknown handwritten target", Overrides{Package: "x", Include: []string{"Widget"}, Handwritten: []string{"Widget.nope"}}},
		{"unknown exclude target", Overrides{Package: "x", Include: []string{"Widget"}, Exclude: []string{"Widget.nope"}}},
		{"binding not in include", Overrides{Package: "x", Include: []string{"WidgetInfo"}, Bindings: []string{"Widget"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ov.Path = "fixture"
			if err := c.ov.Validate(doc); err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

// TestIntersectionFallsBackWhenUnresolvable verifies that GeoUnresolvable
// (an alias intersecting a resolvable ref with one that isn't in the IR at
// all) falls back to the pre-flattening behavior — an opaque js.Value type
// alias, with a warning — rather than silently merging only the fields it
// could resolve (which would misrepresent the shape).
func TestIntersectionFallsBackWhenUnresolvable(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))
	ov := &Overrides{Package: "x", Include: []string{"GeoUnresolvable"}, Path: "fixture"}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Errorf("expected a fallback warning, got none")
	}
	if !strings.Contains(string(result.Source), "type GeoUnresolvable = js.Value") {
		t.Errorf("expected GeoUnresolvable to fall back to js.Value, got:\n%s", result.Source)
	}
}

// TestDeclHasMemberSeesFlattenedFields verifies that an override key can
// reference a field GeoExt only has by virtue of extends-flattening
// (lat, inherited-then-overridden from GeoBase; lng is GeoExt's own), not
// just its own direct members.
func TestDeclHasMemberSeesFlattenedFields(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture.json"))
	ov := &Overrides{
		Package: "x",
		Include: []string{"GeoExt"},
		Rename:  map[string]string{"GeoExt.lat": "Latitude"},
		Path:    "fixture",
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatalf("Validate() failed for a rename targeting a flattened field: %v", err)
	}
}

// TestGenerateGolden2 exercises the cfgen extensions from
// tmp/06-codegen-spec.md section 2.1: overload index/literal splitting with
// a skipped variant (and its warning), typeParam defaults, nullable
// Promise returns for a prim/handle/data type, handle-extends flattening,
// union-of-object-literals data merging (as a top-level alias and as an
// intersection operand), and Iterable<T> params.
func TestGenerateGolden2(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture2.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture2.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	wantWarnings := []string{
		// Encoding (Store.get's overload-2 method typeParam) has no
		// default, so it falls back to js.Value with a warning, same as
		// any other unresolvable typeParam (spec item 2's "no default"
		// case).
		`type parameter "Encoding" used outside of a supported context, falling back to js.Value`,
		`Store.get: skipping overload 0 (2 params) with no overloads: entry`,
	}
	if !slicesEqual(wantWarnings, result.Warnings) {
		t.Errorf("Warnings = %v, want %v", result.Warnings, wantWarnings)
	}

	// tmp/06-codegen-spec.md 1.3's nested-data-type pointer rule: an
	// optional or nullable field whose type is itself an included data
	// type becomes *T (Config.Opt, Config.Nullable), while a required one
	// of the same type stays a plain value (Config.Req), and an optional
	// handle-type ref is unaffected (Config.Owner was already *Parent).
	src := string(result.Source)
	for _, want := range []string{
		"Req      Box     `js:\"req\"`",
		"Opt      *Box    `js:\"opt\"`",
		"Nullable *Box    `js:\"nullable\"`",
		"Owner    *Parent `js:\"owner\"`",
		"if !jsrt.IsNil(v.Get(\"opt\")) {",
		"if o.Opt != nil {",
		"obj.Set(\"opt\", (*o.Opt).toJS())",
		// A types: override naming an included data declaration
		// (Config.altBox -> Box) triggers the same pointer treatment.
		"AltBox *Box `js:\"altBox\"`",
		"if o.AltBox != nil {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q", want)
		}
	}

	goldenPath := filepath.Join("testdata", "fixture2.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

// TestOverloadsRejectsBadEntries covers the overloads: validation errors:
// an out-of-range index, a duplicate index, and a literal that doesn't
// match any parameter of the selected overload (an upstream reordering
// guard, per spec item 1).
// TestGenerateGoldenStreams exercises the stream type mappings from
// tmp/06-codegen-spec.md 3.1.1: a WritableStream getter/return maps to
// io.WriteCloser, a ReadableStream getter/return is unaffected (still
// io.ReadCloser), a ReadableStream parameter maps to io.Reader (via
// jsrt.ReadableStreamFromReader), a WritableStream parameter falls back to
// js.Value, and a types: override can force io.Reader on an otherwise-
// unresolvable union parameter.
func TestGenerateGoldenStreams(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture3.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture3.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	src := string(result.Source)
	for _, want := range []string{
		// property/return position: unaffected by the new parameter-
		// position mapping.
		"func (x *Streams) Sink() io.WriteCloser {",
		"ret = jsrt.WriteCloser(x.v.Get(\"sink\"))",
		"func (x *Streams) Download() (io.ReadCloser, error) {",
		// parameter position: ReadableStream -> io.Reader, converted with
		// jsrt.ReadableStreamFromReader at the call site.
		"func (x *Streams) Upload(body io.Reader) error {",
		"jsrt.Call(x.v, \"upload\", jsrt.ReadableStreamFromReader(body))",
		// parameter position: WritableStream -> js.Value (escape hatch).
		"func (x *Streams) Attach(dest js.Value) error {",
		// a types: override forcing io.Reader on an unresolvable union
		// parameter.
		"func (x *Streams) UploadRaw(data io.Reader) error {",
		"jsrt.Call(x.v, \"uploadRaw\", jsrt.ReadableStreamFromReader(data))",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q", want)
		}
	}

	goldenPath := filepath.Join("testdata", "fixture3.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

// TestGenerateGoldenNestedContainers exercises the nested array/record loop
// variable naming fix: a directly-nested container (e.g. [][]T, or a
// Record<string, []T>) previously reused the same loop/temp variable names
// ("i", "e", "arr", "keys", "k", "vv", "m", "v") at every nesting level,
// so the inner loop's declaration shadowed the outer loop's, corrupting
// both FromJS decoding and ToJS encoding. containerSuffix now gives each
// level beyond the first a distinct suffix.
func TestGenerateGoldenNestedContainers(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture4.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture4.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", result.Warnings)
	}

	src := string(result.Source)
	for _, want := range []string{
		// [][]float64 / [][]string: the outer level keeps the original,
		// unsuffixed names (no golden-file churn for a plain, non-nested
		// []T), and the inner level gets a distinct "1"-suffixed set, so
		// FromJS indexes with both the outer and inner index instead of
		// the inner shadowing the outer (the corruption this fixes).
		"[][]float64", "[][]string",
		"for i := range out.Floats {",
		"for i1 := range out.Floats[i] {",
		"out.Floats[i][i1] = v.Get(\"floats\").Index(i).Index(i1).Float()",
		"for i1, e1 := range e {",
		"arr.SetIndex(i, arr1)",
		"for i := range out.Words {",
		"for i1 := range out.Words[i] {",
		// Record<string, []string> (tags): the record's own level keeps
		// the unsuffixed names, and the nested array value gets the
		// suffixed set.
		"map[string][]string",
		"for i := 0; i < keys.Length(); i++ {",
		"var vv []string",
		"for i1 := range vv {",
		"out.Tags[k] = vv",
		// Array<Record<string, number>> (rows): the array's own level
		// keeps the unsuffixed names, and the nested record gets the
		// suffixed set.
		"[]map[string]float64",
		"for i := range out.Rows {",
		"keys1 := js.Global().Get(\"Object\").Call(\"keys\", v.Get(\"rows\").Index(i))",
		"for i1 := 0; i1 < keys1.Length(); i1++ {",
		"k1 := keys1.Index(i1).String()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q", want)
		}
	}

	goldenPath := filepath.Join("testdata", "fixture4.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

// TestGenerateGoldenMapAndRest exercises the cfgen extensions from
// tmp/06-codegen-spec.md section 4.1: a Map<string, T> return value becomes
// map[string]T (via Array.from(v.keys())/v.get(k), not Object.keys, since a
// JS Map isn't a plain object) — both for a resolvable T (string) and for an
// unresolved typeParam T (falling back to map[string]js.Value, with a
// warning) — and a rest parameter becomes a Go variadic parameter: ...any
// (spread directly into jsrt.Call) when its element type itself maps to
// js.Value, or ...T with an element-by-element []any conversion built ahead
// of the call otherwise.
func TestGenerateGoldenMapAndRest(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture5.json"))
	ov, err := LoadOverrides(filepath.Join("testdata", "fixture5.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ov.Validate(doc); err != nil {
		t.Fatal(err)
	}
	result, err := Generate(doc, ov)
	if err != nil {
		t.Fatal(err)
	}
	wantWarnings := []string{
		`type parameter "T" used outside of a supported context, falling back to js.Value`,
	}
	if !slicesEqual(wantWarnings, result.Warnings) {
		t.Errorf("Warnings = %v, want %v", result.Warnings, wantWarnings)
	}

	src := string(result.Source)
	for _, want := range []string{
		// Map<string, string> -> map[string]string, decoded via
		// Array.from(v.keys())/v.get(k).
		"func (x *Registry) GetAll() (map[string]string, error) {",
		"mapKeys := js.Global().Get(\"Array\").Call(\"from\", r.Call(\"keys\"))",
		"mapKey := mapKeys.Index(mapIdx).String()",
		"mapVal = r.Call(\"get\", mapKey).String()",
		"ret[mapKey] = mapVal",
		// Map<string, T> with unresolved T -> map[string]js.Value.
		"func (x *Registry) GetAllRaw() (map[string]js.Value, error) {",
		// A rest parameter whose element is any becomes ...any. Since it
		// isn't the method's only argument (level precedes it), and Go
		// disallows mixing an individually-listed argument with a trailing
		// spread for the same variadic parameter, level is folded into a
		// []any ahead of the spread rather than listed separately.
		"func (x *Registry) Log(level string, args ...any) error {",
		"jsrt.Call(x.v, \"log\", append([]any{level}, args...)...)",
		// A rest parameter whose element resolves to a concrete Go type
		// becomes ...T, with a []any built ahead of the call, then folded
		// together with the leading name argument the same way.
		"func (x *Registry) Tag(name string, values ...string) error {",
		"arg1 := make([]any, len(values))",
		"for i, e := range values {",
		"arg1[i] = e",
		"jsrt.Call(x.v, \"tag\", append([]any{name}, arg1...)...)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source missing %q", want)
		}
	}

	goldenPath := filepath.Join("testdata", "fixture5.golden.go.txt")
	if *update {
		if err := os.WriteFile(goldenPath, result.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Source) != string(want) {
		t.Errorf("generated output does not match golden file %s (run `go test ./cfgen/gen/... -update` to refresh it if the change is intentional)\n--- got ---\n%s\n--- want ---\n%s", goldenPath, result.Source, want)
	}
}

func TestOverloadsRejectsBadEntries(t *testing.T) {
	doc := loadFixtureIR(t, filepath.Join("testdata", "fixture2.json"))
	cases := []struct {
		name string
		ov   Overrides
	}{
		{
			"index out of range",
			Overrides{Package: "x", Include: []string{"Store", "Parent", "Box"}, Overloads: map[string][]OverloadEntry{
				"Store.get": {{Index: 99, Name: "GetText", Literal: "text"}},
			}},
		},
		{
			"duplicate index",
			Overrides{Package: "x", Include: []string{"Store", "Parent", "Box"}, Overloads: map[string][]OverloadEntry{
				"Store.get": {{Index: 1, Name: "GetText", Literal: "text"}, {Index: 1, Name: "GetText2", Literal: "text"}},
			}},
		},
		{
			"literal mismatch",
			Overrides{Package: "x", Include: []string{"Store", "Parent", "Box"}, Overloads: map[string][]OverloadEntry{
				"Store.get": {{Index: 1, Name: "GetText", Literal: "does-not-exist"}},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.ov.Path = "fixture2"
			if err := c.ov.Validate(doc); err != nil {
				// Index-range/duplicate checks happen in Validate.
				return
			}
			if _, err := Generate(doc, &c.ov); err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPascalCase(t *testing.T) {
	cases := map[string]string{
		"cacheTtl":       "CacheTTL",
		"asOrganization": "AsOrganization",
		"md5":            "MD5",
		"id":             "ID",
		"widget_name":    "WidgetName",
		"success":        "Success",
		"httpStatus":     "HTTPStatus",
	}
	for in, want := range cases {
		if got := pascalCase(in); got != want {
			t.Errorf("pascalCase(%q) = %q, want %q", in, got, want)
		}
	}
}
