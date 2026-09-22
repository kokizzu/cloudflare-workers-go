//go:build js && wasm

package kv

import (
	"testing"

	kvjs "github.com/syumai/workers-go/exp/cloudflare/kv"
)

// TestToListKey checks the small conversion from the generated
// kvjs.KVNamespaceListKey to the L2 *ListKey. The rest of List's behavior
// (paging via limit/prefix/cursor, and the cursor-omitted-on-completion
// case) is covered through the public API in
// TestNamespace_List_prefixLimitCursor, which exercises List end-to-end
// against a fake KVNamespace.
func TestToListKey(t *testing.T) {
	t.Run("with_expiration", func(t *testing.T) {
		got := toListKey(kvjs.KVNamespaceListKey{Name: "foo", Expiration: 123})
		if got.Name != "foo" {
			t.Errorf("Name = %q, want %q", got.Name, "foo")
		}
		if got.Expiration != 123 {
			t.Errorf("Expiration = %d, want %d", got.Expiration, 123)
		}
	})

	t.Run("without_expiration", func(t *testing.T) {
		got := toListKey(kvjs.KVNamespaceListKey{Name: "foo"})
		if got.Name != "foo" {
			t.Errorf("Name = %q, want %q", got.Name, "foo")
		}
		if got.Expiration != 0 {
			t.Errorf("Expiration = %d, want 0", got.Expiration)
		}
	})
}
