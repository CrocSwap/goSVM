package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Strings have no fixed length: wire layouts reject them with a message
// naming the fixed-size alternative (docs/GO_STRINGS.md, SCHEMA2.md).
func TestSchema2RejectsStrings(t *testing.T) {
	const want = "has a variable length; wire layouts are fixed-size, use a [N]byte array"
	for _, tc := range []struct{ field, want string }{
		{"Bad string", "wire field Bad: string " + want},
		{"Bad [2]string", "wire field Bad[]: string " + want},
		{"Bad struct{ S string }", "wire field S: string " + want},
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
