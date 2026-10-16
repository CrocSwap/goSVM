package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// trim is optional; when present each entry must be an import path or an
// import path followed by /... (docs/GO_PACKAGES.md#trimming).
func TestTrimSetting(t *testing.T) {
	dir := starter(t)
	path := filepath.Join(dir, "gosvm.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	set := func(trim []string) (Config, error) {
		var fields map[string]any
		if err := json.Unmarshal(original, &fields); err != nil {
			t.Fatal(err)
		}
		fields["trim"] = trim
		b, _ := json.Marshal(fields)
		if err := os.WriteFile(path, b, 0644); err != nil {
			t.Fatal(err)
		}
		return Load(dir)
	}
	want := []string{"github.com/holiman/uint256", "github.com/KyberNetwork/uniswapv3-sdk-uint256/..."}
	if c, err := set(want); err != nil || !reflect.DeepEqual(c.Trim, want) {
		t.Fatalf("trim %v: %v %v", want, c.Trim, err)
	}
	if _, err := set([]string{"example.com/.../x"}); err == nil || !strings.Contains(err.Error(), "trim pattern") {
		t.Fatalf("bad pattern accepted: %v", err)
	}
	// A project without dependencies to trim generates and checks as before.
	if _, err := set([]string{"example.com/none/..."}); err != nil {
		t.Fatal(err)
	}
	if err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(dir); err != nil || c.Trim != nil {
		t.Fatalf("default trim: %v %v", err, c.Trim)
	}
}
