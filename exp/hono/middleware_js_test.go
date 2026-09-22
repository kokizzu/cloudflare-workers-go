package hono

import (
	"io"
	"strings"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/jsutil"
)

// TestRunHonoMiddleware_callsNext verifies that runHonoMiddleware
// (registered on jsutil.Binding as "runHonoMiddleware" by this package's
// init) builds a *Context from context.ctx (here, a fakeHonoContext acting
// as Hono's own Context - see its doc comment for the asymmetry with
// Cloudflare's ExecutionContext) and calls the Middleware set via the
// package-level middleware variable (ServeMiddleware blocks forever, so
// tests set middleware directly instead), with a next function that invokes
// the JS-side next callback.
func TestRunHonoMiddleware_callsNext(t *testing.T) {
	reqObj := jstest.Request(t, "GET", "http://example.com/path?x=1", nil, nil)
	fake := newFakeHonoContext(t, reqObj)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: fake.Value()})

	var (
		gotPath        string
		calledNextThen bool
	)
	middleware = func(c *Context, next func()) {
		gotPath = c.Request().URL.Path
		next()
		calledNextThen = true
	}
	t.Cleanup(func() { middleware = nil })

	var nextCalls int
	nextFn := jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		nextCalls++
		return jstest.Resolved(js.Undefined())
	})

	p := jstest.Binding(t, "runHonoMiddleware").Invoke(nextFn)
	jstest.Await(t, p)

	if gotPath != "/path" {
		t.Errorf("c.Request().URL.Path = %q, want %q", gotPath, "/path")
	}
	if nextCalls != 1 {
		t.Errorf("JS next callback called %d times, want 1", nextCalls)
	}
	if !calledNextThen {
		t.Errorf("middleware did not resume after calling next()")
	}
}

// TestRunHonoMiddleware_setHeaderStatusBody verifies that Context's
// SetHeader/SetStatus/SetBody reach the underlying Hono Context object's
// header()/status()/body() calls.
func TestRunHonoMiddleware_setHeaderStatusBody(t *testing.T) {
	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	fake := newFakeHonoContext(t, reqObj)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: fake.Value()})

	middleware = func(c *Context, next func()) {
		c.SetHeader("X-Test", "yes")
		c.SetStatus(201)
		c.SetBody(io.NopCloser(strings.NewReader("hi")))
	}
	t.Cleanup(func() { middleware = nil })

	nextFn := jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		return jstest.Resolved(js.Undefined())
	})
	p := jstest.Binding(t, "runHonoMiddleware").Invoke(nextFn)
	jstest.Await(t, p)

	if headerCalls := fake.HeaderCalls(); len(headerCalls) != 1 || headerCalls[0] != [2]string{"X-Test", "yes"} {
		t.Errorf("header() calls = %v, want [[X-Test yes]]", headerCalls)
	}
	if statusCalls := fake.StatusCalls(); len(statusCalls) != 1 || statusCalls[0] != 201 {
		t.Errorf("status() calls = %v, want [201]", statusCalls)
	}
	bodyCalls := fake.BodyCalls()
	if len(bodyCalls) != 1 {
		t.Fatalf("body() calls = %d, want 1", len(bodyCalls))
	}
	if got := string(jstest.ReadAll(t, bodyCalls[0])); got != "hi" {
		t.Errorf("body() argument content = %q, want %q", got, "hi")
	}
}

// TestRunHonoMiddleware_nextRejects verifies that a rejected JS-side
// next() is propagated: the Middleware signature keeps next as func(),
// so runHonoMiddleware captures the error and returns it after the
// middleware finishes, rejecting the Promise returned to the JS caller.
func TestRunHonoMiddleware_nextRejects(t *testing.T) {
	reqObj := jstest.Request(t, "GET", "http://example.com/", nil, nil)
	fake := newFakeHonoContext(t, reqObj)
	jstest.SetRuntimeContext(t, jstest.RuntimeContext{Ctx: fake.Value()})

	var ranPastNext bool
	middleware = func(c *Context, next func()) {
		next()
		// The middleware itself continues running after a rejected
		// next(); the rejection is reported by runHonoMiddleware.
		ranPastNext = true
	}
	t.Cleanup(func() { middleware = nil })

	nextFn := jstest.Func(t, func(_ js.Value, _ []js.Value) any {
		return jstest.Rejected("downstream failed")
	})
	p := jstest.Binding(t, "runHonoMiddleware").Invoke(nextFn)
	if _, err := jsutil.AwaitPromise(p); err == nil {
		t.Fatal("runHonoMiddleware's Promise resolved, want a rejection after next() rejected")
	}
	if !ranPastNext {
		t.Error("middleware did not run past next(); middleware body should still complete before the rejection is reported")
	}
}
