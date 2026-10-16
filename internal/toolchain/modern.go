package toolchain

import (
	"archive/tar"
	"compress/bzip2"
	"context"
	"encoding/json"
	"fmt"
	target "gosvm/internal/targets"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// The official platform-tools v1.57 release asset of each host is pinned
// (size and SHA-256) in archives.go. The extracted Clang/LLD bytes must
// additionally equal target.ModernBackendHashes, the host's content pins.
const (
	SourceOfficialArchive = "official-archive"
	SourceQualifiedLocal  = "qualified-local-backend"
)

// modernPackage describes what a modern backend install must contain. Tests
// substitute a fake package; production uses officialModern.
type modernPackage struct {
	URL     string // download location (a mirror when configured)
	SHA     string
	Size    int64
	Members map[string]string // archive member -> installed path
	Files   map[string]string // installed path -> SHA-256
	Check   func(llvm string) error
}

func officialModern() modernPackage {
	files := map[string]string{}
	for name, hash := range target.ModernBackendHashes {
		files["llvm/bin/"+name] = hash
	}
	a, _ := hostArchive(target.ModernTools)
	return modernPackage{
		URL: downloadURL(target.ModernTools, a), SHA: a.SHA, Size: a.Size,
		// clang is a symlink to clang-22 and ld.lld a symlink to lld; both
		// executables depend only on macOS system libraries (otool -L).
		Members: map[string]string{"llvm/bin/clang-22": "llvm/bin/clang", "llvm/bin/lld": "llvm/bin/ld.lld"},
		Files:   files,
		Check: func(llvm string) error {
			p, _ := target.Resolve(target.Solana)
			return p.CheckBackend(llvm)
		},
	}
}

func ModernRoot() string {
	if p := os.Getenv("GOSVM_CACHE"); p != "" {
		return filepath.Join(p, "toolchains", target.ModernTools, runtime.GOOS+"-"+runtime.GOARCH)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "gosvm/toolchains", target.ModernTools, runtime.GOOS+"-"+runtime.GOARCH)
}

// ModernAvailable reports whether a managed modern backend has been published.
func ModernAvailable() bool {
	root := ModernRoot()
	_, err := os.Stat(filepath.Join(root, "receipt.json"))
	return root != "" && err == nil
}

// VerifyModern accepts either an official-archive install or an imported
// qualified local backend; both must match the frontend's content pins.
func VerifyModern(root string) error {
	_, err := ReadModern(root)
	return err
}

// ReadModern verifies the managed modern backend and returns its receipt.
func ReadModern(root string) (Receipt, error) {
	return verifyModern(root, officialModern())
}

func verifyModern(root string, pkg modernPackage) (Receipt, error) {
	var r Receipt
	if len(pkg.Files) == 0 {
		return r, fmt.Errorf("platform-tools %s has no content pin for host %s (supported hosts: %s)", target.ModernTools, target.Host(), target.ModernHosts)
	}
	b, err := os.ReadFile(filepath.Join(root, "receipt.json"))
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	source := (r.Source == SourceOfficialArchive && r.ArchiveSHA == pkg.SHA) || (r.Source == SourceQualifiedLocal && r.ArchiveSHA == "")
	if r.Version != target.ModernTools || r.Platform != runtime.GOOS+"-"+runtime.GOARCH || !source || len(r.Files) != len(pkg.Files) {
		return r, fmt.Errorf("modern backend receipt mismatch")
	}
	for name, want := range pkg.Files {
		if r.Files[name] != want {
			return r, fmt.Errorf("modern backend content pin mismatch")
		}
		got, err := hashFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return r, err
		}
		if got != want {
			return r, fmt.Errorf("managed backend %s is damaged; remove %s and run gosvm toolchain install", name, root)
		}
	}
	return r, pkg.Check(filepath.Join(root, "llvm"))
}

// InstallModern imports only the two already qualified binaries from an
// existing platform-tools v1.57 installation.
func InstallModern(ctx context.Context, root, llvm string, out io.Writer) error {
	pkg := officialModern()
	if err := pkg.Check(llvm); err != nil {
		return err
	}
	return publishModern(ctx, root, out, pkg, func(_, stage string) (Receipt, error) {
		files := map[string]string{}
		for dest, hash := range pkg.Files {
			if err := ctx.Err(); err != nil {
				return Receipt{}, err
			}
			data, err := os.ReadFile(filepath.Join(llvm, "bin", filepath.Base(dest)))
			if err != nil {
				return Receipt{}, err
			}
			p := filepath.Join(stage, filepath.FromSlash(dest))
			if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				return Receipt{}, err
			}
			if err = os.WriteFile(p, data, 0755); err != nil {
				return Receipt{}, err
			}
			actual, err := hashFile(p)
			if err != nil {
				return Receipt{}, err
			}
			if actual != hash {
				return Receipt{}, fmt.Errorf("backend changed during import")
			}
			files[dest] = hash
		}
		return Receipt{Version: target.ModernTools, Platform: runtime.GOOS + "-" + runtime.GOARCH, Source: SourceQualifiedLocal, Files: files}, nil
	})
}

// InstallModernArchive downloads (archive == "") or reads a local copy of the
// official v1.57 archive, verifies its size and SHA-256 before decompression,
// extracts only Clang and LLD, and requires their exact qualified content.
func InstallModernArchive(ctx context.Context, root, archive string, out io.Writer, download Download) error {
	if _, err := hostArchive(target.ModernTools); err != nil {
		return err
	}
	if target.ModernBackendHashes == nil {
		return fmt.Errorf("platform-tools %s has no content pin for host %s (supported hosts: %s)", target.ModernTools, target.Host(), target.ModernHosts)
	}
	return installModernArchive(ctx, root, archive, out, download, officialModern())
}

func installModernArchive(ctx context.Context, root, archive string, out io.Writer, download Download, pkg modernPackage) error {
	return publishModern(ctx, root, out, pkg, func(tmp, stage string) (Receipt, error) {
		file, err := obtainArchive(ctx, tmp, archive, out, download, pkg.URL, pkg.SHA, pkg.Size,
			fmt.Sprintf("Downloading official platform-tools %s (%.0f MB); only Clang and LLD will be retained.", target.ModernTools, float64(pkg.Size)/1e6),
			"Verifying SHA-256 and extracting Clang and LLD...")
		if err != nil {
			return Receipt{}, err
		}
		f, err := os.Open(file)
		if err != nil {
			return Receipt{}, err
		}
		files, err := extractAllowlist(ctx, tar.NewReader(contextReader{ctx, bzip2.NewReader(contextReader{ctx, f})}), stage, pkg.Members)
		closeErr := f.Close()
		if err != nil {
			return Receipt{}, err
		}
		if closeErr != nil {
			return Receipt{}, closeErr
		}
		// Never execute extracted bytes that differ from the qualified backend.
		for dest, want := range pkg.Files {
			if files[dest] != want {
				return Receipt{}, fmt.Errorf("official archive %s does not match the qualified content pin (got %s, want %s); not installing", dest, files[dest], want)
			}
		}
		return Receipt{Version: target.ModernTools, Platform: runtime.GOOS + "-" + runtime.GOARCH, Source: SourceOfficialArchive, ArchiveSHA: pkg.SHA, Files: files}, nil
	})
}

// obtainArchive downloads into tmp (archive == "") or uses a local copy, and
// verifies the pinned size and SHA-256 before anything decompresses it.
func obtainArchive(ctx context.Context, tmp, archive string, out io.Writer, download Download, url, sha string, size int64, downloading, verifying string) (string, error) {
	file := archive
	if file == "" {
		if download == nil {
			return "", fmt.Errorf("no downloader configured")
		}
		file = filepath.Join(tmp, "platform-tools.tar.bz2")
		fmt.Fprintln(out, downloading)
		f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		w := &archiveWriter{w: f, out: out, size: size, last: time.Now()}
		copyErr := download(ctx, url, w)
		closeErr := f.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	fmt.Fprintln(out, verifying)
	if info, err := os.Stat(file); err != nil {
		return "", err
	} else if info.Size() != size {
		return "", fmt.Errorf("backend archive size %d; expected %d", info.Size(), size)
	}
	digest, err := hashFile(file)
	if err != nil {
		return "", err
	}
	if digest != sha {
		return "", fmt.Errorf("backend archive SHA-256 mismatch: got %s", digest)
	}
	return file, nil
}

// publishModern stages a backend privately and publishes it by rename after the
// receipt verifies. One installer per root; failures never leave a partial root.
func publishModern(ctx context.Context, root string, out io.Writer, pkg modernPackage, fill func(tmp, stage string) (Receipt, error)) error {
	verify := func(dir string) (Receipt, error) { return verifyModern(dir, pkg) }
	r, installed, err := publish(ctx, root, verify, fill)
	if err != nil {
		return err
	}
	if !installed {
		fmt.Fprintln(out, "Qualified backend already installed and verified:", root)
		return nil
	}
	fmt.Fprintf(out, "Installed content-pinned platform-tools %s backend (%s): %s\n", target.ModernTools, r.Source, root)
	return nil
}

// publish verifies an existing root (installed=false) or fills a private stage,
// writes its receipt, verifies it, and publishes it by rename.
func publish(ctx context.Context, root string, verify func(string) (Receipt, error), fill func(tmp, stage string) (Receipt, error)) (Receipt, bool, error) {
	if root == "" {
		return Receipt{}, false, fmt.Errorf("cannot resolve user cache directory; set GOSVM_CACHE")
	}
	if _, err := os.Stat(root); err == nil {
		r, err := verify(root)
		return r, false, err
	} else if !os.IsNotExist(err) {
		return Receipt{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0755); err != nil {
		return Receipt{}, false, err
	}
	lock := root + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return Receipt{}, false, fmt.Errorf("modern installer busy (or interrupted earlier); lock: %s: %w", lock, err)
	}
	defer os.Remove(lock)
	tmp, err := os.MkdirTemp(filepath.Dir(root), ".modern-")
	if err != nil {
		return Receipt{}, false, err
	}
	defer os.RemoveAll(tmp)
	stage := filepath.Join(tmp, "backend")
	if err = os.MkdirAll(stage, 0755); err != nil {
		return Receipt{}, false, err
	}
	r, err := fill(tmp, stage)
	if err != nil {
		return r, false, err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, false, err
	}
	if err = os.WriteFile(filepath.Join(stage, "receipt.json"), append(b, '\n'), 0644); err != nil {
		return r, false, err
	}
	if _, err = verify(stage); err != nil {
		return r, false, err
	}
	if err = ctx.Err(); err != nil {
		return r, false, err
	}
	if err = os.Rename(stage, root); err != nil {
		return r, false, err
	}
	return r, true, nil
}
