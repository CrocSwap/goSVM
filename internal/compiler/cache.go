package compiler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BuildCached keeps a small receipt beside the output. Source and frontend use
// content hashes, backend executables use resolved paths/size/mtime (as in a
// timestamp-based build system). Use an uncached build after deliberately
// replacing a tool while preserving its size and modification timestamp.
func BuildCached(source, output, llvm, arch string) error {
	key, err := buildKey(source, llvm, arch)
	if err != nil {
		return err
	}
	if cacheHit(output, key) {
		return nil
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".gosvm-output-")
	if err != nil {
		return err
	}
	temp := f.Name()
	if err = f.Close(); err != nil {
		return err
	}
	defer os.Remove(temp)
	if err = Build(source, temp, llvm, arch); err != nil {
		return err
	}
	// Hash our own output before publishing it. Concurrent builds may leave a
	// mismatched ELF/receipt pair (a safe cache miss), never a receipt that binds
	// one input to a different build's bytes.
	h, err := fileHash(temp)
	if err != nil {
		return err
	}
	if err = os.Rename(temp, output); err != nil {
		return err
	}
	return writeReceipt(output, key, h)
}

type receipt struct {
	Input  string
	Output string
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func buildKey(source, llvm, arch string) (string, error) {
	compiler, err := os.Executable()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "gosvm-receipt-v2\x00%s\x00", arch)
	sources, err := ReadSources(source)
	if err != nil {
		return "", err
	}
	for _, src := range sources {
		fmt.Fprintf(h, "%s\x00%x\n", filepath.Base(src.Name), sha256.Sum256(src.Data))
	}
	for _, path := range []string{compiler} {
		s, err := fileHash(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintln(h, s)
	}
	for _, name := range []string{"clang", "ld.lld"} {
		p, err := filepath.EvalSymlinks(filepath.Join(llvm, "bin", name))
		if err != nil {
			return "", err
		}
		p, err = filepath.Abs(p)
		if err != nil {
			return "", err
		}
		s, err := os.Stat(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\n", p, s.Size(), s.ModTime().UnixNano())
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func cacheHit(output, key string) bool {
	b, err := os.ReadFile(output + ".gosvm-cache")
	if err != nil {
		return false
	}
	var r receipt
	if json.Unmarshal(b, &r) != nil || r.Input != key {
		return false
	}
	h, err := fileHash(output)
	return err == nil && h == r.Output
}

func saveReceipt(output, key string) error {
	h, err := fileHash(output)
	if err != nil {
		return err
	}
	return writeReceipt(output, key, h)
}

func writeReceipt(output, key, h string) error {
	b, err := json.Marshal(receipt{Input: key, Output: h})
	if err != nil {
		return err
	}
	// An interrupted/torn receipt merely causes a rebuild. The output hash also
	// protects against a replaced/truncated ELF or a competing writer.
	return os.WriteFile(output+".gosvm-cache", append(b, '\n'), 0644)
}
