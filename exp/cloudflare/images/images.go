//go:build js && wasm

package images

import (
	"net/http"

	"github.com/syumai/workers-go/internal/jshttp"
)

// HTTPResponse returns the transformation result as a *http.Response (via
// internal/jshttp.ToResponse), ready to return from a Worker handler or
// store in a cache.
//
// It calls Response with the zero ImageTransformationResponseOptions (no
// extra headers); use Response directly if you need to pass options.
func (r *ImageTransformationResult) HTTPResponse() (*http.Response, error) {
	v, err := r.Response(ImageTransformationResponseOptions{})
	if err != nil {
		return nil, err
	}
	return jshttp.ToResponse(v)
}
