package compiler

import (
	"os"
	"testing"
)

// TestMain removes scratch state shared between tests (the real-library
// module copies, reallibs_diff_test.go).
func TestMain(m *testing.M) {
	code := m.Run()
	if realLibsSetup.dir != "" {
		os.RemoveAll(realLibsSetup.dir)
	}
	os.Exit(code)
}
