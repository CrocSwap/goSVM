package toolchain

import (
	"archive/tar"
	"compress/bzip2"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	target "gosvm/internal/targets"
)

// The Rust library producer needs the complete platform-tools tree (rustc,
// cargo, their sysroot and LLVM utilities), not only the slim Clang/LLD backend.
// It is extracted from the same pinned archive into a separate managed root.

// RustTools names the managed full-tree root (beside the slim v1.57 root).
const RustTools = target.ModernTools + "-full"

// rustDriver is the hash-suffixed rustc driver library in the v1.57 archive.
const rustDriver = "rust/lib/librustc_driver-fd41adedc7c98fbf.dylib"

const (
	maxTreeMember = 1 << 30
	maxTreeBytes  = 4 << 30
)

// fullPackage describes a full-tree install. Files maps paths relative to the
// platform-tools root to their required SHA-256 (symlinks are followed).
type fullPackage struct {
	URL   string
	SHA   string
	Size  int64
	Files map[string]string
}

func officialFull() fullPackage {
	files := map[string]string{}
	for name, hash := range target.ModernBackendHashes {
		files["llvm/bin/"+name] = hash
	}
	paths := map[string]string{"rustc": "rust/bin/rustc", "cargo": "rust/bin/cargo", "librustc_driver": rustDriver, "llvm-objcopy": "llvm/bin/llvm-objcopy", "llvm-ar": "llvm/bin/llvm-ar"}
	for name, hash := range target.RustToolHashes {
		files[paths[name]] = hash
	}
	return fullPackage{URL: ModernArchiveURL, SHA: modernArchiveSHA, Size: modernArchiveSize, Files: files}
}

// RustRoot is the managed full-tree root; its platform-tools/ subdirectory is
// the tree and receipt.json records the archive and content pins.
func RustRoot() string {
	if p := os.Getenv("GOSVM_CACHE"); p != "" {
		return filepath.Join(p, "toolchains", RustTools, runtime.GOOS+"-"+runtime.GOARCH)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "gosvm/toolchains", RustTools, runtime.GOOS+"-"+runtime.GOARCH)
}

// RustPlatformTools returns the managed platform-tools tree when its receipt
// matches the current pins, else "". It does not hash; consumers that execute
// the tools (the Rust producer) verify the pinned bytes themselves.
func RustPlatformTools() string {
	root := RustRoot()
	if root == "" {
		return ""
	}
	if _, err := verifyFull(root, officialFull(), false); err != nil {
		return ""
	}
	return filepath.Join(root, "platform-tools")
}

// RustInstalled reports whether a managed full tree has been published.
func RustInstalled() bool {
	root := RustRoot()
	_, err := os.Stat(filepath.Join(root, "receipt.json"))
	return root != "" && err == nil
}

// ReadRust verifies the managed full tree (receipt and pinned bytes).
func ReadRust(root string) (Receipt, error) {
	return verifyFull(root, officialFull(), true)
}

func verifyFull(root string, pkg fullPackage, deep bool) (Receipt, error) {
	var r Receipt
	b, err := os.ReadFile(filepath.Join(root, "receipt.json"))
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	if r.Version != target.ModernTools || r.Platform != runtime.GOOS+"-"+runtime.GOARCH || r.Source != SourceOfficialArchive || r.ArchiveSHA != pkg.SHA || len(r.Files) != len(pkg.Files) {
		return r, fmt.Errorf("Rust toolchain receipt mismatch")
	}
	for name, want := range pkg.Files {
		if r.Files[name] != want {
			return r, fmt.Errorf("Rust toolchain content pin mismatch")
		}
	}
	if !deep {
		return r, nil
	}
	got, err := hashTreeFiles(filepath.Join(root, "platform-tools"), pkg.Files)
	if err != nil {
		return r, fmt.Errorf("managed Rust toolchain is damaged (%v); remove %s and run gosvm toolchain install -rust", err, root)
	}
	for name, want := range pkg.Files {
		if got[name] != want {
			return r, fmt.Errorf("managed Rust toolchain %s is damaged; remove %s and run gosvm toolchain install -rust", name, root)
		}
	}
	return r, nil
}

// hashTreeFiles hashes the named files, following symlinks only within tree.
func hashTreeFiles(tree string, files map[string]string) (map[string]string, error) {
	base, err := filepath.EvalSymlinks(tree)
	if err != nil {
		return nil, err
	}
	got := map[string]string{}
	for name := range files {
		p, err := filepath.EvalSymlinks(filepath.Join(tree, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(base, p); err != nil || !safeRel(filepath.ToSlash(rel)) {
			return nil, fmt.Errorf("%s resolves outside the toolchain", name)
		}
		if got[name], err = hashFile(p); err != nil {
			return nil, err
		}
	}
	return got, nil
}

// InstallRustArchive installs the full platform-tools tree for the Rust
// producer from the official archive (archive == "" downloads it), and the
// slim Clang/LLD backend too when missing, using one download for both.
func InstallRustArchive(ctx context.Context, archive string, out io.Writer, download Download) error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("platform-tools %s is currently qualified on macOS arm64 only", target.ModernTools)
	}
	return installRust(ctx, ModernRoot(), RustRoot(), archive, out, download, officialModern(), officialFull())
}

func installRust(ctx context.Context, slim, full, archive string, out io.Writer, download Download, mp modernPackage, fp fullPackage) error {
	if slim == "" || full == "" {
		return fmt.Errorf("cannot resolve user cache directory; set GOSVM_CACHE")
	}
	_, slimErr := os.Stat(slim)
	_, fullErr := os.Stat(full)
	if archive == "" && os.IsNotExist(slimErr) && os.IsNotExist(fullErr) {
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp(filepath.Dir(full), ".download-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		if archive, err = obtainArchive(ctx, tmp, "", out, download, fp.URL, fp.SHA, fp.Size,
			fmt.Sprintf("Downloading official platform-tools %s (%.0f MB) for the backend and the Rust library producer.", target.ModernTools, float64(fp.Size)/1e6),
			"Verified archive SHA-256."); err != nil {
			return err
		}
	}
	if err := installModernArchive(ctx, slim, archive, out, download, mp); err != nil {
		return err
	}
	return installFullArchive(ctx, full, archive, out, download, fp)
}

func installFullArchive(ctx context.Context, root, archive string, out io.Writer, download Download, pkg fullPackage) error {
	verify := func(dir string) (Receipt, error) { return verifyFull(dir, pkg, true) }
	r, installed, err := publish(ctx, root, verify, func(tmp, stage string) (Receipt, error) {
		file, err := obtainArchive(ctx, tmp, archive, out, download, pkg.URL, pkg.SHA, pkg.Size,
			fmt.Sprintf("Downloading official platform-tools %s (%.0f MB) for the Rust library producer.", target.ModernTools, float64(pkg.Size)/1e6),
			"Verifying SHA-256 and extracting the full platform-tools tree...")
		if err != nil {
			return Receipt{}, err
		}
		f, err := os.Open(file)
		if err != nil {
			return Receipt{}, err
		}
		tree := filepath.Join(stage, "platform-tools")
		err = extractTree(ctx, tar.NewReader(contextReader{ctx, bzip2.NewReader(contextReader{ctx, f})}), tree)
		closeErr := f.Close()
		if err != nil {
			return Receipt{}, err
		}
		if closeErr != nil {
			return Receipt{}, closeErr
		}
		got, err := hashTreeFiles(tree, pkg.Files)
		if err != nil {
			return Receipt{}, fmt.Errorf("official archive lacks a pinned Rust toolchain file: %w", err)
		}
		for name, want := range pkg.Files {
			if got[name] != want {
				return Receipt{}, fmt.Errorf("official archive %s does not match the qualified content pin (got %s, want %s); not installing", name, got[name], want)
			}
		}
		return Receipt{Version: target.ModernTools, Platform: runtime.GOOS + "-" + runtime.GOARCH, Source: SourceOfficialArchive, ArchiveSHA: pkg.SHA, Files: got}, nil
	})
	if err != nil {
		return err
	}
	if !installed {
		fmt.Fprintln(out, "Rust toolchain already installed and verified:", root)
		return nil
	}
	fmt.Fprintf(out, "Installed content-pinned platform-tools %s Rust toolchain (%s): %s\n", r.Version, r.Source, filepath.Join(root, "platform-tools"))
	return nil
}

// safeRel reports whether a slash path is relative and stays inside its root.
func safeRel(n string) bool {
	return n != "" && n != "." && n != ".." && !path.IsAbs(n) && path.Clean(n) == n && !strings.HasPrefix(n, "../")
}

// safeLink accepts a relative link target of the form ../../a/b: leading ".."
// steps (taken from real directories, since nothing is written beneath a
// link) that stay inside the tree, then forward components only. Forward steps
// through other links stay inside because those links are checked the same way.
func safeLink(name, link string) bool {
	if link == "" || path.IsAbs(link) || path.Clean(link) != link {
		return false
	}
	parts := strings.Split(link, "/")
	up := 0
	for up < len(parts) && parts[up] == ".." {
		up++
	}
	for _, c := range parts[up:] {
		if c == ".." || c == "." || c == "" {
			return false
		}
	}
	j := path.Join(path.Dir(name), link)
	return up < len(parts) && up <= strings.Count(name, "/") && (j == "." || safeRel(j))
}

// extractTree writes every directory, regular file and symlink of the archive
// under dest. Paths and link targets must stay inside dest; nothing is written
// beneath a symlink; other member types are refused.
func extractTree(ctx context.Context, tr *tar.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	links := map[string]bool{}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		n := strings.TrimSuffix(strings.TrimPrefix(h.Name, "./"), "/")
		if n == "" && h.Typeflag == tar.TypeDir {
			continue
		}
		if !safeRel(n) || links[n] {
			return fmt.Errorf("invalid archive member %q", h.Name)
		}
		for p := path.Dir(n); p != "."; p = path.Dir(p) {
			if links[p] {
				return fmt.Errorf("archive member %q is beneath a symlink", h.Name)
			}
		}
		p := filepath.Join(dest, filepath.FromSlash(n))
		switch h.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(p, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if h.Size < 0 || h.Size > maxTreeMember || total+h.Size > maxTreeBytes {
				return fmt.Errorf("archive member %q exceeds size limits", h.Name)
			}
			total += h.Size
			if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if h.Mode&0111 != 0 {
				mode = 0755
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, contextReader{ctx, tr})
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if !safeLink(n, h.Linkname) {
				return fmt.Errorf("archive symlink %q -> %q escapes the toolchain", h.Name, h.Linkname)
			}
			if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				return err
			}
			if err = os.Symlink(h.Linkname, p); err != nil {
				return err
			}
			links[n] = true
		default:
			return fmt.Errorf("unsupported archive member %q (type %q)", h.Name, h.Typeflag)
		}
	}
	// Defense in depth: links that resolve must resolve physically inside dest.
	// (The official archive has a dangling lldb link; safeLink keeps it inert.)
	base, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	for n := range links {
		p, err := filepath.EvalSymlinks(filepath.Join(dest, filepath.FromSlash(n)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("archive symlink %q: %w", n, err)
		}
		if rel, err := filepath.Rel(base, p); err != nil || (rel != "." && !safeRel(filepath.ToSlash(rel))) {
			return fmt.Errorf("archive symlink %q resolves outside the toolchain", n)
		}
	}
	return nil
}
