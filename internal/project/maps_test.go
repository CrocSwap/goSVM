package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Maps have no fixed size: wire layouts reject them with a message naming
// the fixed-size alternative (docs/GO_MAPS.md, SCHEMA2.md).
func TestSchema2RejectsMaps(t *testing.T) {
	const want = "is a map; wire layouts are fixed-size, use a [N] array of key/value structs"
	for _, tc := range []struct{ field, want string }{
		{"Bad map[uint64]uint64", "wire field Bad: map[uint64]uint64 " + want},
		{"Bad [2]map[uint8]uint8", "wire field Bad[]: map[uint8]uint8 " + want},
		{"Bad struct{ M map[[4]byte]int64 }", "wire field M: map[[4]byte]int64 " + want},
	} {
		dir := filepath.Join(t.TempDir(), "multi-state")
		if err := NewModuleSDK(dir, "example.org/multi-state", MultiSchema, 2); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "model/model.go")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, bytes.Replace(b, []byte("Balance uint64"), []byte("Balance uint64\n\t"+tc.field), 1), 0600); err != nil {
			t.Fatal(err)
		}
		if err := Generate(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want %q", tc.field, err, tc.want)
		}
	}
}
