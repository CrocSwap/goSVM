package toolchain

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	target "gosvm/internal/targets"
)

func bzipTar(t *testing.T, items ...member) []byte {
	t.Helper()
	bz, err := exec.LookPath("bzip2")
	if err != nil {
		t.Skip("bzip2 unavailable")
	}
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, m := range items {
		size := int64(len(m.body))
		if m.kind == tar.TypeSymlink {
			size = 0
		}
		if err := w.WriteHeader(&tar.Header{Name: m.name, Mode: 0755, Typeflag: m.kind, Linkname: m.link, Size: size}); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			w.Write([]byte(m.body))
		}
	}
	w.Close()
	cmd := exec.Command(bz, "-c")
	cmd.Stdin = &b
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sum(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// fakeModern describes an archive like the official one: clang/ld.lld are
// symlinks to the real executables, plus unrelated members that must be skipped.
func fakeModern(t *testing.T, clang string) ([]byte, modernPackage) {
	data := bzipTar(t,
		member{"llvm/bin/clang", "", "clang-22", tar.TypeSymlink},
		member{"llvm/bin/ld.lld", "", "lld", tar.TypeSymlink},
		member{"llvm/bin/clang-22", clang, "", tar.TypeReg},
		member{"llvm/bin/lld", "lld", "", tar.TypeReg},
		member{"rust/bin/rustc", "rust", "", tar.TypeReg})
	return data, modernPackage{
		URL: "https://example.invalid/platform-tools.tar.bz2", SHA: sum(data), Size: int64(len(data)),
		Members: map[string]string{"llvm/bin/clang-22": "llvm/bin/clang", "llvm/bin/lld": "llvm/bin/ld.lld"},
		Files:   map[string]string{"llvm/bin/clang": sum([]byte("clang")), "llvm/bin/ld.lld": sum([]byte("lld"))},
		Check:   func(string) error { return nil },
	}
}

func fetcher(data []byte, calls *int) Download {
	return func(_ context.Context, _ string, w io.Writer) error {
		*calls++
		_, err := w.Write(data)
		return err
	}
}

func TestModernDownloadInstallsOnlyPinnedBackend(t *testing.T) {
	data, pkg := fakeModern(t, "clang")
	root := filepath.Join(t.TempDir(), "v1.57")
	calls := 0
	if err := installModernArchive(context.Background(), root, "", io.Discard, fetcher(data, &calls), pkg); err != nil {
		t.Fatal(err)
	}
	r, err := verifyModern(root, pkg)
	if err != nil || r.Source != SourceOfficialArchive || r.ArchiveSHA != pkg.SHA || calls != 1 {
		t.Fatalf("receipt %+v err %v calls %d", r, err, calls)
	}
	if _, err := os.Stat(filepath.Join(root, "rust")); !os.IsNotExist(err) {
		t.Fatal("extracted unrelated member")
	}
	if info, err := os.Lstat(filepath.Join(root, "llvm/bin/clang")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("clang is not the regular pinned executable")
	}
	// Idempotent: verified existing install does not download again.
	if err := installModernArchive(context.Background(), root, "", io.Discard, fetcher(data, &calls), pkg); err != nil || calls != 1 {
		t.Fatalf("reinstall: %v calls=%d", err, calls)
	}
	os.WriteFile(filepath.Join(root, "llvm/bin/ld.lld"), []byte("edited"), 0755)
	if _, err := verifyModern(root, pkg); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("damaged backend accepted: %v", err)
	}
}

func TestModernLocalArchiveAndRejections(t *testing.T) {
	data, pkg := fakeModern(t, "clang")
	dir := t.TempDir()
	archive := filepath.Join(dir, "local.tar.bz2")
	os.WriteFile(archive, data, 0600)
	root := filepath.Join(dir, "ok")
	if err := installModernArchive(context.Background(), root, archive, io.Discard, nil, pkg); err != nil {
		t.Fatal(err)
	}
	// Archive verified but its binaries are not the qualified content.
	other, otherPkg := fakeModern(t, "different clang")
	otherPkg.Files = pkg.Files
	calls := 0
	for name, tc := range map[string]struct {
		data []byte
		pkg  modernPackage
		want string
	}{
		"truncated":   {data[:len(data)-1], pkg, "size"},
		"corrupt":     {append(append([]byte{}, data[:len(data)-1]...), data[len(data)-1]^1), pkg, "SHA-256"},
		"content-pin": {other, otherPkg, "content pin"},
	} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "installed")
			err := installModernArchive(context.Background(), root, "", io.Discard, fetcher(tc.data, &calls), tc.pkg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("accepted bad archive: %v", err)
			}
			entries, _ := os.ReadDir(filepath.Dir(root))
			if len(entries) != 0 {
				t.Fatalf("left state after failure: %v", entries)
			}
		})
	}
	// Oversized downloads stop at the pinned size.
	root = filepath.Join(t.TempDir(), "big")
	big := append(append([]byte{}, data...), 0)
	if err := installModernArchive(context.Background(), root, "", io.Discard, fetcher(big, &calls), pkg); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized: %v", err)
	}
}

func TestModernReceiptSources(t *testing.T) {
	_, pkg := fakeModern(t, "clang")
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "llvm/bin"), 0755)
	os.WriteFile(filepath.Join(root, "llvm/bin/clang"), []byte("clang"), 0755)
	os.WriteFile(filepath.Join(root, "llvm/bin/ld.lld"), []byte("lld"), 0755)
	write := func(r Receipt) {
		b, _ := json.Marshal(r)
		os.WriteFile(filepath.Join(root, "receipt.json"), b, 0644)
	}
	base := Receipt{Version: target.ModernTools, Platform: runtime.GOOS + "-" + runtime.GOARCH, Files: pkg.Files}
	for _, tc := range []struct {
		source, sha string
		ok          bool
	}{
		{SourceQualifiedLocal, "", true}, {SourceOfficialArchive, pkg.SHA, true},
		{SourceOfficialArchive, "", false}, {SourceQualifiedLocal, pkg.SHA, false}, {"other", "", false},
	} {
		r := base
		r.Source, r.ArchiveSHA = tc.source, tc.sha
		write(r)
		if _, err := verifyModern(root, pkg); (err == nil) != tc.ok {
			t.Fatalf("%s/%q: %v", tc.source, tc.sha, err)
		}
	}
}

func TestOfficialModernPins(t *testing.T) {
	pkg := officialModern()
	host, err := hostArchive(target.ModernTools)
	if err != nil {
		t.Skip(err)
	}
	if pkg.Size != host.Size || pkg.SHA != host.SHA || len(pkg.SHA) != 64 || len(pkg.Files) != 2 || pkg.Files["llvm/bin/clang"] != target.ModernBackendHashes["clang"] || pkg.Files["llvm/bin/ld.lld"] != target.ModernBackendHashes["ld.lld"] {
		t.Fatalf("%+v", pkg)
	}
	for _, dest := range pkg.Members {
		if _, ok := pkg.Files[dest]; !ok {
			t.Fatalf("member %s not content pinned", dest)
		}
	}
}
