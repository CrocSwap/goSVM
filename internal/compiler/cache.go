package compiler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BuildCached keeps a small receipt beside the output. Source and frontend use
// content hashes, including backend executable bytes. Replaced tools cannot
// reuse a receipt even when their size and modification timestamp are preserved.
func BuildCached(source, output, llvm, arch string) error {
	return BuildCachedWithOptions(source, output, llvm, arch, BuildOptions{})
}

// BuildCachedWithOptions is BuildCached with compiler options; they are
// part of the receipt key.
func BuildCachedWithOptions(source, output, llvm, arch string, o BuildOptions) error {
	program, err := readProgramWith(source, o)
	if err != nil {
		return err
	}
	key, err := buildProgramKey(program, llvm, arch)
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
	if err = buildProgram(program, temp, llvm, arch); err != nil {
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
	program, err := ReadProgram(source)
	if err != nil {
		return "", err
	}
	return buildProgramKey(program, llvm, arch)
}

// DependencyKey separates reusable output namespaces across frontend/SDK,
// imported Go packages, module resolution and pinned foreign-library changes.
// Application edits reuse that namespace and invalidate the normal build receipt.
func DependencyKey(source string) (string, error) {
	program, err := ReadProgram(source)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "gosvm-dependencies-v1\x00%d\x00%x\n", program.SDK, sha256.Sum256([]byte(sdkSource(program.SDK))))
	for _, unit := range program.Packages {
		if unit.Path == program.Entry {
			continue
		}
		fmt.Fprintf(h, "package:%s\x00", unit.Path)
		for _, src := range unit.Sources {
			fmt.Fprintf(h, "%s\x00%x\n", filepath.Base(src.Name), sha256.Sum256(src.Data))
		}
	}
	for _, src := range program.Metadata {
		fmt.Fprintf(h, "%s\x00%x\n", src.Name, sha256.Sum256(src.Data))
	}
	if err = writeStdlibIdentity(h, program); err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	frontend, err := fileHash(exe)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(h, frontend)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
func buildProgramKey(program *Program, llvm, arch string) (string, error) {
	compiler, err := os.Executable()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "gosvm-receipt-v3\x00%s\x00%s\x00", arch, program.Entry)
	fmt.Fprintf(h, "sdk:%d\x00%x\n", program.SDK, sha256.Sum256([]byte(sdkSource(program.SDK))))
	if program.HeapBytes != 0 {
		fmt.Fprintf(h, "heap:%d\n", program.HeapBytes)
	}
	for _, unit := range program.Packages {
		fmt.Fprintf(h, "package:%s\x00", unit.Path)
		for _, src := range unit.Sources {
			fmt.Fprintf(h, "%s\x00%x\n", filepath.Base(src.Name), sha256.Sum256(src.Data))
		}
	}
	for _, src := range program.Metadata {
		fmt.Fprintf(h, "module:%s\x00%x\n", src.Name, sha256.Sum256(src.Data))
	}
	if err = writeStdlibIdentity(h, program); err != nil {
		return "", err
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
		digest, err := fileHash(p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%s\n", p, s.Size(), s.ModTime().UnixNano(), digest)
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

// writeStdlibIdentity adds the identity of the standard-library overlay
// packages the program reaches (docs/GO_STDLIB.md) to a receipt or
// dependency key; programs that import none keep their earlier keys.
func writeStdlibIdentity(h io.Writer, program *Program) error {
	paths, err := ProgramStdlib(program)
	if err != nil {
		return err
	}
	if id := StdlibIdentity(paths); id != "" {
		fmt.Fprintf(h, "stdlib:%s\x00%s\x00%s\n", StdlibVersion, strings.Join(paths, ","), id)
	}
	return nil
}
