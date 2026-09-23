// Package genericfunc is a genforward test fixture (used by gen_test.go): a
// small, self-contained module whose generic functions exercise every shape
// renderGenericFuncWrapper must support -- type parameters and constraints
// (including a union constraint and the predeclared "any"), parameters
// (including a variadic tail and a blank/unnamed one), multiple results, no
// results, and both a same-package and a cross-package parameter/result
// type. It has its own go.mod so `go build ./...` and `go vet ./...` at the
// repository root skip it, the same way internal/cmd/genforward/mirror-tests'
// fixtures do.
package genericfunc

import (
	"context"
	"encoding/json"
)

// Box is referenced by the generic functions below to exercise
// same-package type qualification: an argument or result type declared in
// this package must become src.Box in the generated wrapper, not Box.
type Box struct {
	V int
}

// MethodLike mirrors exp/cloudflare/rpc.MethodJSON's shape: one type
// parameter, a func-typed parameter that itself references two other
// packages (context and encoding/json), and a same-package result type.
func MethodLike[Out any](fn func(ctx context.Context, args []json.RawMessage) (Out, error)) Box {
	return Box{}
}

// DoLike mirrors exp/cloudflare/workflows.DoJSON's shape: one type
// parameter, a same-package pointer parameter, and two results.
func DoLike[T any](b *Box, name string, fn func(ctx context.Context) (T, error)) (T, error) {
	var zero T
	return zero, nil
}

// Sum has two type parameters (one with a union constraint), a variadic
// final parameter, and a blank first parameter name, which must be renamed
// in the wrapper since a blank identifier can't be used as a value in the
// forwarding call.
func Sum[T ~int | ~float64, N any](_ N, nums ...T) T {
	var total T
	for _, v := range nums {
		total += v
	}
	return total
}

// Noop has a type parameter but no results, exercising the zero-result
// wrapper shape.
func Noop[T any](v T) {}
