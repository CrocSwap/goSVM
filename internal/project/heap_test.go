package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// heap_bytes is optional; when present it must be a RequestHeapFrame size.
func TestHeapBytesSetting(t *testing.T) {
	dir := starter(t)
	path := filepath.Join(dir, "gosvm.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	set := func(n int) error {
		var fields map[string]any
		if err := json.Unmarshal(original, &fields); err != nil {
			t.Fatal(err)
		}
		fields["heap_bytes"] = n
		b, _ := json.Marshal(fields)
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
		c, err := Load(dir)
		if err == nil && c.HeapBytes != n {
			t.Fatalf("heap_bytes %d loaded as %d", n, c.HeapBytes)
		}
		return err
	}
	for _, n := range []int{32768, 65536, 262144} {
		if err := set(n); err != nil {
			t.Fatalf("heap_bytes %d: %v", n, err)
		}
	}
	for _, n := range []int{1024, 32769, 263168} {
		if err := set(n); err == nil || !strings.Contains(err.Error(), "must be a multiple of 1024 from 32768 to 262144") {
			t.Fatalf("heap_bytes %d accepted: %v", n, err)
		}
	}
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(dir); err != nil || c.HeapBytes != 0 {
		t.Fatalf("default heap: %v %d", err, c.HeapBytes)
	}
}
