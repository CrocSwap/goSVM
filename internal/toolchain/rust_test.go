package toolchain

import (
	"archive/tar"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	target "gosvm/internal/targets"
)

// fakeTree resembles the official archive: a "./" root, directories, symlinked
// clang/ld.lld, a Rust compiler and unpinned files that must all be kept.
func fakeTree(t *testing.T, extra ...member) ([]byte, modernPackage, fullPackage) {
	items := append([]member{
		{"./", "", "", tar.TypeDir},
		{"./llvm/bin/", "", "", tar.TypeDir},
		{"./llvm/bin/clang-22", "clang", "", tar.TypeReg},
		{"./llvm/bin/lld", "lld", "", tar.TypeReg},
		{"./llvm/bin/clang", "", "clang-22", tar.TypeSymlink},
		{"./llvm/bin/ld.lld", "", "lld", tar.TypeSymlink},
		{"./llvm/lib/python/lldb/argdumper", "", "../../../bin/lld", tar.TypeSymlink},
		// The official archive has one dangling (inside) link; it is kept.
		{"./llvm/lib/python/lldb/missing", "", "../../../bin/lldb-argdumper", tar.TypeSymlink},
		{"./rust/bin/rustc", "rustc", "", tar.TypeReg},
		{"./rust/lib/rustlib/sbpf-solana-solana/lib/libcore.rlib", "core", "", tar.TypeReg},
		{"./version.md", "v1.57", "", tar.TypeReg},
	}, extra...)
	data := bzipTar(t, items...)
	mp := modernPackage{
		URL: "https://example.invalid/platform-tools.tar.bz2", SHA: sum(data), Size: int64(len(data)),
		Members: map[string]string{"llvm/bin/clang-22": "llvm/bin/clang", "llvm/bin/lld": "llvm/bin/ld.lld"},
		Files:   map[string]string{"llvm/bin/clang": sum([]byte("clang")), "llvm/bin/ld.lld": sum([]byte("lld"))},
		Check:   func(string) error { return nil },
	}
	fp := fullPackage{URL: mp.URL, SHA: mp.SHA, Size: mp.Size, Files: map[string]string{
		"llvm/bin/clang": sum([]byte("clang")), "llvm/bin/ld.lld": sum([]byte("lld")), "rust/bin/rustc": sum([]byte("rustc")),
	}}
	return data, mp, fp
}

func TestRustFullTreeInstall(t *testing.T) {
	data, mp, fp := fakeTree(t)
	dir := t.TempDir()
	slim, full := filepath.Join(dir, "v1.57"), filepath.Join(dir, "v1.57-full")
	calls := 0
	// One download installs both the slim backend and the full tree.
	if err := installRust(context.Background(), slim, full, "", io.Discard, fetcher(data, &calls), mp, fp); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("downloads = %d", calls)
	}
	if _, err := verifyModern(slim, mp); err != nil {
		t.Fatal(err)
	}
	r, err := verifyFull(full, fp, true)
	if err != nil || r.ArchiveSHA != fp.SHA || len(r.Files) != 3 {
		t.Fatalf("receipt %+v: %v", r, err)
	}
	tree := filepath.Join(full, "platform-tools")
	for _, name := range []string{"version.md", "rust/lib/rustlib/sbpf-solana-solana/lib/libcore.rlib"} {
		if _, err := os.Stat(filepath.Join(tree, name)); err != nil {
			t.Fatalf("unpinned member not kept: %v", err)
		}
	}
	if link, err := os.Readlink(filepath.Join(tree, "llvm/bin/clang")); err != nil || link != "clang-22" {
		t.Fatalf("symlink not preserved: %q %v", link, err)
	}
	if info, _ := os.Stat(filepath.Join(tree, "rust/bin/rustc")); info.Mode().Perm() != 0755 {
		t.Fatalf("mode %v", info.Mode())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("left staging state: %v", entries)
	}
	// Idempotent: nothing is downloaded again.
	if err := installRust(context.Background(), slim, full, "", io.Discard, fetcher(data, &calls), mp, fp); err != nil || calls != 1 {
		t.Fatalf("reinstall: %v calls=%d", err, calls)
	}
	os.WriteFile(filepath.Join(tree, "rust/bin/rustc"), []byte("edited"), 0755)
	if _, err := verifyFull(full, fp, true); err == nil || !strings.Contains(err.Error(), "install -rust") {
		t.Fatalf("damaged tree accepted: %v", err)
	}
	if _, err := verifyFull(full, fp, false); err != nil {
		t.Fatalf("receipt-only check: %v", err)
	}
}

func TestRustFullTreeLocalArchiveWithExistingBackend(t *testing.T) {
	data, mp, fp := fakeTree(t)
	dir := t.TempDir()
	archive := filepath.Join(dir, "local.tar.bz2")
	os.WriteFile(archive, data, 0600)
	slim, full := filepath.Join(dir, "v1.57"), filepath.Join(dir, "v1.57-full")
	if err := installModernArchive(context.Background(), slim, archive, io.Discard, nil, mp); err != nil {
		t.Fatal(err)
	}
	if err := installRust(context.Background(), slim, full, archive, io.Discard, nil, mp, fp); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyFull(full, fp, true); err != nil {
		t.Fatal(err)
	}
}

func TestRustFullTreeRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		extra []member
		pin   bool
		want  string
	}{
		"escaping-path":     {[]member{{"../evil", "x", "", tar.TypeReg}}, false, "invalid"},
		"absolute-path":     {[]member{{"/evil", "x", "", tar.TypeReg}}, false, "invalid"},
		"absolute-link":     {[]member{{"llvm/bin/evil", "", "/etc/passwd", tar.TypeSymlink}}, false, "escapes"},
		"escaping-link":     {[]member{{"llvm/bin/evil", "", "../../../x", tar.TypeSymlink}}, false, "escapes"},
		"beneath-link":      {[]member{{"rust/dir", "", "lib", tar.TypeSymlink}, {"rust/dir/x", "x", "", tar.TypeReg}}, false, "beneath"},
		"link-through-link": {[]member{{"rust/up", "", "../llvm", tar.TypeSymlink}, {"rust/evil", "", "up/../..", tar.TypeSymlink}}, false, "escapes"},
		"hardlink":          {[]member{{"rust/hard", "", "rust/bin/rustc", tar.TypeLink}}, false, "unsupported"},
		"content-pin":       {nil, true, "content pin"},
	} {
		t.Run(name, func(t *testing.T) {
			data, _, fp := fakeTree(t, tc.extra...)
			if tc.pin {
				fp.Files["rust/bin/rustc"] = sum([]byte("other rustc"))
			}
			dir := t.TempDir()
			full := filepath.Join(dir, "v1.57-full")
			calls := 0
			err := installFullArchive(context.Background(), full, "", io.Discard, fetcher(data, &calls), fp)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("accepted bad archive: %v", err)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("left state after failure: %v", entries)
			}
		})
	}
}

func TestRustPlatformToolsReceipt(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GOSVM_CACHE", cache)
	if RustPlatformTools() != "" || RustInstalled() {
		t.Fatal("reported a missing tree")
	}
	pkg := officialFull()
	root := RustRoot()
	if root != filepath.Join(cache, "toolchains", "v1.57-full", runtime.GOOS+"-"+runtime.GOARCH) {
		t.Fatal(root)
	}
	os.MkdirAll(root, 0755)
	write := func(r Receipt) {
		b, _ := json.Marshal(r)
		os.WriteFile(filepath.Join(root, "receipt.json"), b, 0644)
	}
	r := Receipt{Version: target.ModernTools, Platform: runtime.GOOS + "-" + runtime.GOARCH, Source: SourceOfficialArchive, ArchiveSHA: pkg.SHA, Files: pkg.Files}
	write(r)
	if RustPlatformTools() != filepath.Join(root, "platform-tools") {
		t.Fatal("matching receipt not selected")
	}
	r.ArchiveSHA = ""
	write(r)
	if RustPlatformTools() != "" {
		t.Fatal("mismatched receipt selected")
	}
}

func TestOfficialFullPins(t *testing.T) {
	pkg := officialFull()
	if pkg.SHA != officialModern().SHA || len(pkg.Files) != len(target.ModernBackendHashes)+len(target.RustToolHashes) {
		t.Fatalf("%+v", pkg)
	}
	for name, hash := range officialModern().Files {
		if pkg.Files[name] != hash {
			t.Fatalf("%s not pinned like the backend", name)
		}
	}
	for _, p := range []string{"rust/bin/rustc", "rust/bin/cargo", rustDriver, "llvm/bin/llvm-objcopy", "llvm/bin/llvm-ar"} {
		if len(pkg.Files[p]) != 64 {
			t.Fatalf("%s not pinned", p)
		}
	}
}
