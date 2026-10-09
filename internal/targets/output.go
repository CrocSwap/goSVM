package target

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// OutputDir separates ISA/profile and exact backend bytes. Source/library content
// is additionally bound by the compiler receipt, so edits invalidate cached builds.
func (p Profile) OutputDir(root, llvm string) (string, error) {
	h := sha256.New()
	for _, name := range []string{"clang", "ld.lld"} {
		b, err := os.ReadFile(filepath.Join(llvm, "bin", name))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(b)
		h.Write(sum[:])
	}
	return filepath.Join(root, "build", p.Name, p.Tools, fmt.Sprintf("%x", h.Sum(nil))[:16]), nil
}
