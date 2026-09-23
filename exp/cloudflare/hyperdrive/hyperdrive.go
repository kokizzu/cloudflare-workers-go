//go:build js && wasm

package hyperdrive

import (
	"context"
	"net"
	"strconv"

	"github.com/syumai/workers-go/cloudflare/sockets"
)

// Connect dials Hyperdrive's origin database over cloudflare/sockets,
// bridging the synchronous JS `Hyperdrive.connect()` Socket (which
// z hyperdrive_gen.go leaves handwritten -- see the package doc comment)
// to a net.Conn. It is equivalent to
//
//	sockets.Connect(ctx, net.JoinHostPort(h.Host(), strconv.Itoa(int(h.Port()))), nil)
//
// since Hyperdrive's connect() docs describe it as returning "the exact
// same socket you'd get from calling connect() [cloudflare/sockets.Connect]
// with the Hyperdrive configuration's host and port" --
// https://developers.cloudflare.com/hyperdrive/examples/connect-to-hyperdrive/connect-to-mysql/.
//
// Use it as a database/sql driver's custom dialer, the way
// _examples/mysql-blog-server's app/handler.go uses cloudflare/sockets.Connect
// directly (RegisterDialContext), except with h.Host()/h.Port() supplying
// the address instead of an env var:
//
//	mysql.RegisterDialContext("tcp", func(ctx context.Context, addr string) (net.Conn, error) {
//		return h.Connect(ctx)
//	})
//	db, err := sql.Open("mysql", h.ConnectionString())
func (h *Hyperdrive) Connect(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(h.Host(), strconv.Itoa(int(h.Port())))
	return sockets.Connect(ctx, addr, nil)
}
