package jshttp

import (
	"net/http"
	"strings"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
)

// ToHeader converts JavaScript sides Headers to http.Header.
//   - Headers: https://developer.mozilla.org/docs/Web/API/Headers
func ToHeader(headers js.Value) http.Header {
	h := http.Header{}
	// Set-Cookie values can themselves contain a comma (e.g. an Expires
	// date), so they cannot be reconstructed by splitting the combined
	// entries() value. getSetCookie() (Fetch standard; workerd and
	// Node 20+) returns each cookie separately. When it is unavailable,
	// fall back to the combined entries() value, which at least keeps a
	// single cookie intact.
	hasGetSetCookie := headers.Get("getSetCookie").Type() == js.TypeFunction
	var combinedSetCookie string
	entries := jsutil.ArrayFrom(headers.Call("entries"))
	headerLen := entries.Length()
	for i := 0; i < headerLen; i++ {
		entry := entries.Index(i)
		key := entry.Index(0).String()
		values := entry.Index(1).String()
		if strings.EqualFold(key, "set-cookie") {
			combinedSetCookie = values
			continue
		}
		// entries() joins repeated values for the same name with ", ";
		// split them back into separate values and trim the join
		// whitespace.
		for _, value := range strings.Split(values, ",") {
			h.Add(key, strings.TrimSpace(value))
		}
	}
	if hasGetSetCookie {
		cookies := jsutil.ArrayFrom(headers.Call("getSetCookie"))
		for i := 0; i < cookies.Length(); i++ {
			h.Add("Set-Cookie", cookies.Index(i).String())
		}
	} else if combinedSetCookie != "" {
		h.Add("Set-Cookie", combinedSetCookie)
	}
	return h
}

// ToJSHeader converts http.Header to JavaScript sides Headers.
//   - Headers: https://developer.mozilla.org/docs/Web/API/Headers
func ToJSHeader(header http.Header) js.Value {
	h := jsutil.HeadersClass.New()
	for key, values := range header {
		for _, value := range values {
			h.Call("append", key, value)
		}
	}
	return h
}
