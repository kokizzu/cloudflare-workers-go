package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// srcAlias is the stable local name genforward uses for the aliased import of
// the corresponding source package in every generated file.
const srcAlias = "src"

// writePackage generates the forwarding file(s) for one package and writes
// them under outDir.
func writePackage(outDir string, p *pkgInfo) error {
	newImportPath := p.importPath // the source module already *is* the new import path
	pkgDir := outDir
	if p.relDir != "" {
		pkgDir = filepath.Join(outDir, filepath.FromSlash(p.relDir))
	}
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return err
	}

	if p.hostOK {
		if err := writeForwardFile(pkgDir, "forward.go", p, newImportPath, p.common, "", true); err != nil {
			return err
		}
		if len(p.jsOnly) > 0 {
			if err := writeForwardFile(pkgDir, "forward_js.go", p, newImportPath, p.jsOnly, "//go:build js && wasm\n", false); err != nil {
				return err
			}
		}
		return nil
	}

	// The source package does not build on host at all (e.g. it imports
	// syscall/js unconditionally, as every non-root package in this codebase
	// does). Its forwarder is written, unconstrained, into forward.go too:
	// it will fail to build on host exactly like the source package does,
	// which is expected and not an error condition for genforward.
	return writeForwardFile(pkgDir, "forward.go", p, newImportPath, p.all, "", true)
}

func writeForwardFile(pkgDir, filename string, p *pkgInfo, newImportPath string, syms []symbol, buildTag string, includeDoc bool) error {
	var b bytes.Buffer

	if buildTag != "" {
		b.WriteString(buildTag)
		b.WriteString("\n")
	}

	if includeDoc {
		writeDocComment(&b, p.doc, newImportPath)
	}

	fmt.Fprintf(&b, "package %s\n\n", p.name)

	// Generic functions are rendered as wrapper functions (see
	// renderGenericFuncWrapper) whose signatures may reference types from
	// packages other than the source package; ic collects those as a side
	// effect of printing them, so the import block below can list them
	// alongside the srcAlias import.
	ic := newImportCollector(p.importPath)
	wrappers := make(map[string]string, len(syms))
	for _, s := range syms {
		if s.kind != kindFunc || s.genFunc == nil {
			continue
		}
		text, err := renderGenericFuncWrapper(s.genFunc, ic)
		if err != nil {
			return fmt.Errorf("rendering generic wrapper for %s: %w", s.name, err)
		}
		wrappers[s.name] = text
	}

	if len(syms) > 0 {
		b.WriteString("import (\n")
		fmt.Fprintf(&b, "\t%s %q\n", srcAlias, p.importPath)
		extraPaths := make([]string, 0, len(ic.aliases))
		for path := range ic.aliases {
			extraPaths = append(extraPaths, path)
		}
		sort.Strings(extraPaths)
		for _, path := range extraPaths {
			fmt.Fprintf(&b, "\t%s %q\n", ic.aliases[path], path)
		}
		b.WriteString(")\n\n")
	}

	for _, s := range syms {
		switch s.kind {
		case kindType:
			fmt.Fprintf(&b, "type %s = %s.%s\n", s.name, srcAlias, s.name)
		case kindFunc:
			if text, ok := wrappers[s.name]; ok {
				b.WriteString(text)
			} else {
				fmt.Fprintf(&b, "var %s = %s.%s\n", s.name, srcAlias, s.name)
			}
		case kindConst:
			fmt.Fprintf(&b, "const %s = %s.%s\n", s.name, srcAlias, s.name)
		case kindVar:
			fmt.Fprintf(&b, "var %s = %s.%s\n", s.name, srcAlias, s.name)
		}
	}

	out, err := format.Source(b.Bytes())
	if err != nil {
		return fmt.Errorf("formatting %s: %w\n---\n%s", filename, err, b.String())
	}
	return os.WriteFile(filepath.Join(pkgDir, filename), out, 0o644)
}

// writeDocComment writes the package doc comment: the original doc (if any)
// followed by a "Deprecated:" paragraph pointing at newImportPath, so
// pkg.go.dev shows the deprecation notice for this package.
func writeDocComment(b *bytes.Buffer, originalDoc, newImportPath string) {
	if originalDoc != "" {
		for _, line := range strings.Split(strings.TrimRight(originalDoc, "\n"), "\n") {
			if line == "" {
				b.WriteString("//\n")
			} else {
				b.WriteString("// ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
		b.WriteString("//\n")
	}
	fmt.Fprintf(b, "// Deprecated: use %s instead.\n", newImportPath)
}

// importCollector prints go/types types via types.TypeString, mapping
// references to the source package itself to srcAlias, and every other
// package to an import alias it records. Alias collisions across distinct
// import paths (e.g. two packages both named "v1") are resolved by
// appending "_" until the alias is free.
type importCollector struct {
	srcPkgPath string
	aliases    map[string]string // import path -> alias
	used       map[string]string // alias -> import path
}

func newImportCollector(srcPkgPath string) *importCollector {
	return &importCollector{
		srcPkgPath: srcPkgPath,
		aliases:    make(map[string]string),
		used:       map[string]string{srcAlias: srcPkgPath},
	}
}

// qualifier is a types.Qualifier: it's passed to types.TypeString to decide
// how a named type's package is printed.
func (ic *importCollector) qualifier(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	if pkg.Path() == ic.srcPkgPath {
		return srcAlias
	}
	if alias, ok := ic.aliases[pkg.Path()]; ok {
		return alias
	}
	alias := pkg.Name()
	for {
		existing, taken := ic.used[alias]
		if !taken || existing == pkg.Path() {
			break
		}
		alias += "_"
	}
	ic.aliases[pkg.Path()] = alias
	ic.used[alias] = pkg.Path()
	return alias
}

// typeString prints t, qualifying package-level names per ic.qualifier
// (recording any newly seen import as a side effect).
func (ic *importCollector) typeString(t types.Type) string {
	return types.TypeString(t, ic.qualifier)
}

// anyConstraint is the type of the predeclared identifier "any", used to
// normalize an empty-interface type-parameter constraint (however it was
// spelled in the source) to "any" in generated wrappers.
var anyConstraint = types.Universe.Lookup("any").Type()

// constraintString prints a type parameter's constraint, preferring the
// "any" spelling over the equivalent "interface{}" when the two are
// identical.
func (ic *importCollector) constraintString(t types.Type) string {
	if types.Identical(t, anyConstraint) {
		return "any"
	}
	return ic.typeString(t)
}

// paramNames chooses a Go identifier for every parameter of sig, preferring
// each parameter's original name. A blank ("" or "_") name, or one that
// collides with another parameter's name, is replaced with a fresh "argN"
// name (this can only happen for a name genforward itself generated, since
// the source signature -- being valid Go -- cannot declare two named
// parameters with the same name). Renaming is required because, unlike a
// parameter declaration, a blank identifier can't be used as a value in the
// wrapper's forwarding call.
func paramNames(sig *types.Signature) []string {
	params := sig.Params()
	n := params.Len()
	names := make([]string, n)
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		name := params.At(i).Name()
		if name != "" && name != "_" && !seen[name] {
			names[i] = name
			seen[name] = true
		}
	}
	next := 0
	for i := 0; i < n; i++ {
		if names[i] != "" {
			continue
		}
		for {
			cand := fmt.Sprintf("arg%d", next)
			next++
			if !seen[cand] {
				names[i] = cand
				seen[cand] = true
				break
			}
		}
	}
	return names
}

// renderGenericFuncWrapper renders a type-parameterized wrapper function
// forwarding to fn, e.g. for
//
//	func MethodJSON[Out any](fn func(ctx context.Context, args []json.RawMessage) (Out, error)) Method
//
// in the source package:
//
//	func MethodJSON[Out any](fn func(ctx context.Context, args []json.RawMessage) (Out, error)) src.Method {
//		return src.MethodJSON[Out](fn)
//	}
//
// A generic function can't be forwarded with `var F = src.F` (a var
// declaration can't carry type parameters), so this reproduces fn's
// signature -- type parameters, constraints, parameter types (including a
// variadic final parameter), and result types -- via ic, which also
// records any import the signature requires beyond the source package
// itself.
func renderGenericFuncWrapper(fn *types.Func, ic *importCollector) (string, error) {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return "", fmt.Errorf("%s: object is not a func", fn.Name())
	}
	tps := sig.TypeParams()
	if tps.Len() == 0 {
		return "", fmt.Errorf("%s: not a generic function signature", fn.Name())
	}

	names := paramNames(sig)
	params := sig.Params()
	results := sig.Results()

	var b strings.Builder

	fmt.Fprintf(&b, "func %s[", fn.Name())
	for i := 0; i < tps.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		tp := tps.At(i)
		fmt.Fprintf(&b, "%s %s", tp.Obj().Name(), ic.constraintString(tp.Constraint()))
	}
	b.WriteString("](")

	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		typ := params.At(i).Type()
		if sig.Variadic() && i == params.Len()-1 {
			elem := typ.(*types.Slice).Elem()
			fmt.Fprintf(&b, "%s ...%s", names[i], ic.typeString(elem))
		} else {
			fmt.Fprintf(&b, "%s %s", names[i], ic.typeString(typ))
		}
	}
	b.WriteString(")")

	switch results.Len() {
	case 0:
	case 1:
		fmt.Fprintf(&b, " %s", ic.typeString(results.At(0).Type()))
	default:
		b.WriteString(" (")
		for i := 0; i < results.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(ic.typeString(results.At(i).Type()))
		}
		b.WriteString(")")
	}

	b.WriteString(" {\n\t")
	if results.Len() > 0 {
		b.WriteString("return ")
	}
	fmt.Fprintf(&b, "%s.%s[", srcAlias, fn.Name())
	for i := 0; i < tps.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(tps.At(i).Obj().Name())
	}
	b.WriteString("](")
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(names[i])
		if sig.Variadic() && i == params.Len()-1 {
			b.WriteString("...")
		}
	}
	b.WriteString(")\n}\n\n")

	return b.String(), nil
}
