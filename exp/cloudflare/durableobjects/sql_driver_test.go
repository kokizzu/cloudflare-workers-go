//go:build js && wasm

package durableobjects

import (
	"context"
	"syscall/js"
	"testing"
	"time"

	"github.com/syumai/workers-go/internal/jsutil"
)

// fakeCursorValue builds a JS object shaped like a SqlStorageCursor good
// enough for sql_driver.go: columnNames (a plain array property, matching
// the real cursor), rowsWritten (a plain number property), and raw() (a
// method -- called via ExecContext/QueryContext, matching
// tmp/06-codegen-spec.md 5.2 item 3's "cursor.JSValue().Call(\"raw\")"
// approach for a member the generator excludes).
func fakeCursorValue(columnNames []string, rows [][]any, rowsWritten float64) js.Value {
	names := make([]any, len(columnNames))
	for i, n := range columnNames {
		names[i] = n
	}
	rawRows := make([]any, len(rows))
	for i, row := range rows {
		rawRows[i] = row
	}
	rawArray := js.ValueOf(rawRows)

	cursor := jsutil.NewObject()
	cursor.Set("columnNames", js.ValueOf(names))
	cursor.Set("rowsWritten", rowsWritten)
	cursor.Set("raw", js.FuncOf(func(this js.Value, args []js.Value) any {
		return rawArray
	}))
	return cursor
}

// TestSQLDriver_ExecContext_PassesBindings verifies that OpenDB's driver
// converts every supported bind value type into what SqlStorage.exec
// expects, and that the query text is forwarded verbatim.
func TestSQLDriver_ExecContext_PassesBindings(t *testing.T) {
	var gotQuery string
	var gotArgs []js.Value
	fakeStorage := jsutil.NewObject()
	fakeStorage.Set("exec", js.FuncOf(func(this js.Value, args []js.Value) any {
		gotQuery = args[0].String()
		gotArgs = append([]js.Value(nil), args[1:]...)
		return fakeCursorValue([]string{"id"}, nil, 1)
	}))

	db := SQLStorageFromJS(fakeStorage).OpenDB()
	defer db.Close()

	when := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	const query = "INSERT INTO t (a, b, c, d, e, f) VALUES (?, ?, ?, ?, ?, ?)"
	res, err := db.ExecContext(context.Background(), query,
		"hello", int64(42), 3.5, true, []byte{1, 2, 3}, when)
	if err != nil {
		t.Fatalf("ExecContext failed: %v", err)
	}
	if gotQuery != query {
		t.Errorf("query = %q, want %q", gotQuery, query)
	}
	if len(gotArgs) != 6 {
		t.Fatalf("exec called with %d bindings, want 6", len(gotArgs))
	}
	if got := gotArgs[0].String(); got != "hello" {
		t.Errorf("binding[0] = %q, want %q", got, "hello")
	}
	if got := gotArgs[1].Float(); got != 42 {
		t.Errorf("binding[1] = %v, want 42", got)
	}
	if got := gotArgs[2].Float(); got != 3.5 {
		t.Errorf("binding[2] = %v, want 3.5", got)
	}
	if got := gotArgs[3].Bool(); !got {
		t.Errorf("binding[3] = %v, want true", got)
	}
	if got := gotArgs[4].Get("constructor").Get("name").String(); got != "Uint8Array" {
		t.Errorf("binding[4] is not a Uint8Array: %v", gotArgs[4])
	}
	if want := when.Format(time.RFC3339Nano); gotArgs[5].String() != want {
		t.Errorf("binding[5] = %q, want %q", gotArgs[5].String(), want)
	}

	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected failed: %v", err)
	}
	if n != 1 {
		t.Errorf("RowsAffected = %d, want 1", n)
	}
	if _, err := res.LastInsertId(); err == nil {
		t.Error("LastInsertId succeeded, want an error (not supported)")
	}
}

// TestSQLDriver_QueryContext_DecodesRows verifies that QueryContext
// materializes the cursor's raw() iterator in column order and that Scan
// decodes each SqlStorageValue correctly.
func TestSQLDriver_QueryContext_DecodesRows(t *testing.T) {
	fakeStorage := jsutil.NewObject()
	fakeStorage.Set("exec", js.FuncOf(func(this js.Value, args []js.Value) any {
		return fakeCursorValue([]string{"id", "name"}, [][]any{
			{1.0, "alice"},
			{2.0, "bob"},
		}, 0)
	}))

	db := SQLStorageFromJS(fakeStorage).OpenDB()
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT id, name FROM t")
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("Columns failed: %v", err)
	}
	if len(cols) != 2 || cols[0] != "id" || cols[1] != "name" {
		t.Errorf("Columns() = %v, want [id name]", cols)
	}

	var names []string
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("Scan failed: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}
	if len(names) != 2 || names[0] != "alice" || names[1] != "bob" {
		t.Errorf("scanned names = %v, want [alice bob]", names)
	}
}

// TestSQLDriver_TransactionsUnsupported verifies Begin errors instead of
// silently pretending to support transactions (matching cloudflare/d1).
func TestSQLDriver_TransactionsUnsupported(t *testing.T) {
	db := SQLStorageFromJS(jsutil.NewObject()).OpenDB()
	defer db.Close()

	if _, err := db.Begin(); err == nil {
		t.Error("Begin succeeded, want an error (transactions not supported)")
	}
}
