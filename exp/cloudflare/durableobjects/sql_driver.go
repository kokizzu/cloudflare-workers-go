//go:build js && wasm

package durableobjects

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"syscall/js"
	"time"

	"github.com/syumai/workers-go/exp/internal/jsrt"
)

// OpenDB returns a *sql.DB backed by s, the Durable Object's embedded SQLite
// database
// (https://developers.cloudflare.com/durable-objects/api/sql-storage/).
//
// Every database/sql call runs synchronously against SqlStorage.exec (via
// s.Exec) -- there is no separate network connection to open, and no
// transaction support (Begin/BeginTx always error, matching
// cloudflare/d1's driver, which this one otherwise mirrors). Placeholders
// are "?"; bind values must be string, int64, float64, bool, []byte, nil,
// or time.Time (encoded as an RFC 3339 string, since SqlStorage's bindings
// have no native date/time type).
func (s *SQLStorage) OpenDB() *sql.DB {
	return sql.OpenDB(&sqlConnector{storage: s})
}

type sqlConnector struct {
	storage *SQLStorage
}

var _ driver.Connector = (*sqlConnector)(nil)

func (c *sqlConnector) Connect(context.Context) (driver.Conn, error) {
	return &sqlConn{storage: c.storage}, nil
}

func (c *sqlConnector) Driver() driver.Driver {
	return &sqlDriver{}
}

// sqlDriver only exists to satisfy driver.Connector.Driver; sql.OpenDB
// (which OpenDB uses, unlike sql.Open) never calls back into it, since it
// addresses the database purely through the driver.Connector already in
// hand.
type sqlDriver struct{}

var _ driver.Driver = (*sqlDriver)(nil)

func (d *sqlDriver) Open(name string) (driver.Conn, error) {
	return nil, errors.New("durableobjects: sql: Open is not supported; use (*SQLStorage).OpenDB")
}

type sqlConn struct {
	storage *SQLStorage
}

var (
	_ driver.Conn               = (*sqlConn)(nil)
	_ driver.ConnPrepareContext = (*sqlConn)(nil)
	_ driver.ConnBeginTx        = (*sqlConn)(nil)
)

func (c *sqlConn) Prepare(query string) (driver.Stmt, error) {
	return &sqlStmt{storage: c.storage, query: query}, nil
}

func (c *sqlConn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return c.Prepare(query)
}

func (c *sqlConn) Close() error {
	// SqlStorage has no connection to release.
	return nil
}

func (c *sqlConn) Begin() (driver.Tx, error) {
	return nil, errors.New("durableobjects: sql: transactions are not currently supported")
}

func (c *sqlConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, errors.New("durableobjects: sql: transactions are not currently supported")
}

type sqlStmt struct {
	storage *SQLStorage
	query   string
}

var (
	_ driver.Stmt             = (*sqlStmt)(nil)
	_ driver.StmtExecContext  = (*sqlStmt)(nil)
	_ driver.StmtQueryContext = (*sqlStmt)(nil)
)

func (s *sqlStmt) Close() error { return nil }

// NumInput is not supported and always returns -1 (database/sql skips its
// own argument-count check in that case), matching cloudflare/d1.
func (s *sqlStmt) NumInput() int { return -1 }

func (s *sqlStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("durableobjects: sql: Exec is deprecated and not implemented; use ExecContext")
}

func (s *sqlStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("durableobjects: sql: Query is deprecated and not implemented; use QueryContext")
}

// ExecContext runs the statement via SqlStorage.exec (s.storage.Exec), which
// is synchronous, so ctx is not otherwise consulted.
func (s *sqlStmt) ExecContext(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
	bindings, err := bindingsFromNamedValues(args)
	if err != nil {
		return nil, err
	}
	cursor, err := s.storage.Exec(s.query, bindings...)
	if err != nil {
		return nil, err
	}
	return &sqlResult{cursor: cursor}, nil
}

// QueryContext runs the statement via SqlStorage.exec, then materializes
// the cursor's rows in column order via its raw() iterator (excluded from
// the generated bindings -- see tmp/06-codegen-spec.md 4.1's overrides
// table -- so it's called directly here), combined with ColumnNames() for
// driver.Rows.Columns.
func (s *sqlStmt) QueryContext(_ context.Context, args []driver.NamedValue) (driver.Rows, error) {
	bindings, err := bindingsFromNamedValues(args)
	if err != nil {
		return nil, err
	}
	cursor, err := s.storage.Exec(s.query, bindings...)
	if err != nil {
		return nil, err
	}
	rawIter, err := jsrt.Call(cursor.JSValue(), "raw")
	if err != nil {
		return nil, err
	}
	rawArray, err := jsrt.Call(js.Global().Get("Array"), "from", rawIter)
	if err != nil {
		return nil, err
	}
	return &sqlRows{columns: cursor.ColumnNames(), rowsArray: rawArray}, nil
}

// bindingsFromNamedValues converts database/sql's already-normalized
// driver.NamedValue arguments (string, int64, float64, bool, []byte,
// time.Time, or nil -- database/sql's default ValueConverter guarantees
// this) into the []any SqlStorage.Exec's variadic bindings parameter
// expects, matching cfgen's rest-parameter mapping (see
// tmp/06-codegen-spec.md 4.1).
func bindingsFromNamedValues(args []driver.NamedValue) ([]any, error) {
	bindings := make([]any, len(args))
	for i, a := range args {
		v, err := sqlBindingValue(a.Value)
		if err != nil {
			return nil, fmt.Errorf("durableobjects: sql: bind arg %d: %w", i+1, err)
		}
		bindings[i] = v
	}
	return bindings, nil
}

func sqlBindingValue(v driver.Value) (any, error) {
	switch x := v.(type) {
	case nil, string, int64, float64, bool:
		return x, nil
	case []byte:
		return jsrt.BytesToJS(x), nil
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano), nil
	default:
		return nil, fmt.Errorf("unsupported bind value type %T", v)
	}
}

type sqlResult struct {
	cursor *SQLStorageCursor
}

var _ driver.Result = (*sqlResult)(nil)

// LastInsertId is not supported: SqlStorageCursor has no equivalent of
// D1's `meta.last_row_id` (matching cloudflare/d1's own result.go
// aside -- unlike d1, there isn't even a code path that could support it
// here).
func (r *sqlResult) LastInsertId() (int64, error) {
	return 0, errors.New("durableobjects: sql: LastInsertId is not supported")
}

// RowsAffected returns the cursor's RowsWritten().
func (r *sqlResult) RowsAffected() (int64, error) {
	return int64(r.cursor.RowsWritten()), nil
}

type sqlRows struct {
	columns   []string
	rowsArray js.Value

	once    sync.Once
	rowsLen int
	current int
}

var _ driver.Rows = (*sqlRows)(nil)

func (r *sqlRows) Columns() []string { return r.columns }

func (r *sqlRows) Close() error { return nil }

func (r *sqlRows) Next(dest []driver.Value) error {
	r.once.Do(func() { r.rowsLen = r.rowsArray.Length() })
	if r.current == r.rowsLen {
		return io.EOF
	}
	rowVal := r.rowsArray.Index(r.current)
	for i := range dest {
		dest[i] = decodeSQLStorageValue(rowVal.Index(i))
	}
	r.current++
	return nil
}
