package toolchain

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type member struct {
	name, body, link string
	kind             byte
}

func archive(t *testing.T, items ...member) *tar.Reader {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, m := range items {
		size := int64(len(m.body))
		if m.kind == tar.TypeSymlink {
			size = 0
		}
		if e := w.WriteHeader(&tar.Header{Name: m.name, Mode: 0755, Typeflag: m.kind, Linkname: m.link, Size: size}); e != nil {
			t.Fatal(e)
		}
		if size > 0 {
			if _, e := w.Write([]byte(m.body)); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return tar.NewReader(bytes.NewReader(b.Bytes()))
}
func TestAllowlistExtraction(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "backend")
	tr := archive(t, member{"../../outside", "bad", "", tar.TypeReg}, member{"llvm/bin/clang", "", "/outside", tar.TypeSymlink}, member{"./llvm/bin/clang-19", "clang", "", tar.TypeReg}, member{"llvm/bin/lld", "lld", "", tar.TypeReg}, member{"rust/bin/rustc", "rust", "", tar.TypeReg})
	files, e := extract(context.Background(), tr, root)
	if e != nil {
		t.Fatal(e)
	}
	if len(files) != 2 {
		t.Fatalf("files=%v", files)
	}
	for _, p := range []string{"llvm/bin/clang", "llvm/bin/ld.lld"} {
		h, e := hashFile(filepath.Join(root, p))
		if e != nil || h != files[p] {
			t.Fatalf("hash mismatch: %v", e)
		}
	}
	if _, e := os.Stat(filepath.Join(root, "rust")); !os.IsNotExist(e) {
		t.Fatal("extracted Rust")
	}
	if _, e := os.Stat(filepath.Join(dir, "outside")); !os.IsNotExist(e) {
		t.Fatal("archive escaped destination")
	}
}
func TestRejectUnsafeOrIncompleteBackend(t *testing.T) {
	clang := member{"llvm/bin/clang-19", "clang", "", tar.TypeReg}
	lld := member{"llvm/bin/lld", "lld", "", tar.TypeReg}
	for name, items := range map[string][]member{"missing": {clang}, "duplicate": {clang, lld, clang}, "symlink": {{"llvm/bin/clang-19", "", "/bad", tar.TypeSymlink}, lld}} {
		t.Run(name, func(t *testing.T) {
			if _, e := extract(context.Background(), archive(t, items...), t.TempDir()); e == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := extract(ctx, archive(t, clang, lld), t.TempDir()); e != context.Canceled {
		t.Fatalf("cancellation: %v", e)
	}
}
func TestBadArchiveDoesNotPublish(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.tar.bz2")
	os.WriteFile(f, []byte("not an archive"), 0600)
	root := filepath.Join(dir, "installed")
	var out bytes.Buffer
	e := Install(context.Background(), root, f, &out, nil)
	if e == nil {
		t.Fatal("accepted invalid checksum")
	}
	if !strings.Contains(e.Error(), "SHA-256 mismatch") && !strings.Contains(e.Error(), "macOS arm64 only") {
		t.Fatal(e)
	}
	if _, e := os.Stat(root); !os.IsNotExist(e) {
		t.Fatal("published partial installation")
	}
	if _, e := os.Stat(root + ".lock"); !os.IsNotExist(e) {
		t.Fatal("left lock after failure")
	}
}
