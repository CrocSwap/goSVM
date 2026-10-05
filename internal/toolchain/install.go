// Package toolchain installs a small, pinned SBF backend without Cargo.
package toolchain

import (
	"archive/tar"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Version = "v1.51"

// Pinned from the official release's GitHub asset digest, not a checksum fetched
// beside an arbitrary user-supplied download. Add platforms after validating the
// two-binary package and its dynamic dependencies on that platform.
const archiveSHA = "a1e32b3137fe8199fa76da81c43ec0122bdbd27acf1279f38d8d265f7a6e6cb3"
const archiveSize int64 = 448012397
const archiveURL = "https://github.com/anza-xyz/platform-tools/releases/download/v1.51/platform-tools-osx-aarch64.tar.bz2"

type Receipt struct {
	Version    string            `json:"version"`
	Platform   string            `json:"platform"`
	Source     string            `json:"source"`
	ArchiveSHA string            `json:"archive_sha256"`
	Files      map[string]string `json:"files"`
}

// Root is separate from Solana's cache; installing never alters existing tools.
func Root() string {
	if p := os.Getenv("GOSVM_CACHE"); p != "" {
		return filepath.Join(p, "toolchains", Version, runtime.GOOS+"-"+runtime.GOARCH)
	}
	cache, e := os.UserCacheDir()
	if e != nil {
		return ""
	}
	return filepath.Join(cache, "gosvm", "toolchains", Version, runtime.GOOS+"-"+runtime.GOARCH)
}
func LLVM() string { return filepath.Join(Root(), "llvm") }
func Available() bool {
	_, e := os.Stat(filepath.Join(Root(), "receipt.json"))
	return Root() != "" && e == nil
}

func hashFile(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Verify checks installed bytes against the receipt from the verified archive.
// This detects corruption, not an attacker able to rewrite both files and receipt.
func Verify(root string) (Receipt, error) {
	var r Receipt
	b, e := os.ReadFile(filepath.Join(root, "receipt.json"))
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if r.Version != Version || r.Platform != runtime.GOOS+"-"+runtime.GOARCH || r.ArchiveSHA != archiveSHA || r.Source != archiveURL || len(r.Files) != 2 {
		return r, fmt.Errorf("managed toolchain receipt mismatch")
	}
	for _, p := range []string{"llvm/bin/clang", "llvm/bin/ld.lld"} {
		h, e := hashFile(filepath.Join(root, p))
		if e != nil {
			return r, e
		}
		if h != r.Files[p] {
			return r, fmt.Errorf("managed backend %s is damaged; remove this toolchain directory and reinstall", p)
		}
	}
	return r, nil
}

// Install downloads to a temporary directory, verifies before parsing, extracts
// only regular allowlisted files, smoke-tests the backend, then publishes by rename.
// Cancellation or failure never leaves a partial backend selected by builds.
type Download func(context.Context, string, io.Writer) error

func Install(ctx context.Context, root, archive string, out io.Writer, download Download) error {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("managed installation is currently verified on macOS arm64 only; set SBF_LLVM to your existing v1.51 backend")
	}
	if root == "" {
		return fmt.Errorf("cannot resolve user cache directory; set GOSVM_CACHE")
	}
	if _, e := os.Stat(root); e == nil {
		if _, e = Verify(root); e != nil {
			return e
		}
		fmt.Fprintf(out, "Toolchain already installed and verified: %s\n", root)
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	parent := filepath.Dir(root)
	if e := os.MkdirAll(parent, 0755); e != nil {
		return e
	}
	// One writer per version/platform; an interrupted process can leave only this
	// small lock directory. Do not guess whether a concurrent installer is alive.
	lock := root + ".lock"
	if e := os.Mkdir(lock, 0700); e != nil {
		return fmt.Errorf("installer busy (or interrupted earlier); lock: %s: %w", lock, e)
	}
	defer os.Remove(lock)
	tmp, e := os.MkdirTemp(parent, ".install-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	downloaded := archive
	if downloaded == "" {
		downloaded = filepath.Join(tmp, "platform-tools.tar.bz2")
		fmt.Fprintf(out, "Downloading official platform-tools %s (448 MB); only Clang and LLD will be retained.\n", Version)
		if download == nil {
			return fmt.Errorf("no downloader configured")
		}
		f, e := os.OpenFile(downloaded, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		w := &archiveWriter{w: f, out: out, last: time.Now()}
		copyErr := download(ctx, archiveURL, w)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if w.n != archiveSize {
			return fmt.Errorf("backend archive size %d; expected %d", w.n, archiveSize)
		}

	}
	fmt.Fprintln(out, "Verifying SHA-256 and extracting the minimal backend...")
	digest, e := hashFile(downloaded)
	if e != nil {
		return e
	}
	if digest != archiveSHA {
		return fmt.Errorf("backend archive SHA-256 mismatch: got %s", digest)
	}
	f, e := os.Open(downloaded)
	if e != nil {
		return e
	}
	stage := filepath.Join(tmp, "backend")
	files, e := extract(ctx, tar.NewReader(contextReader{ctx, bzip2.NewReader(contextReader{ctx, f})}), stage)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = smoke(ctx, stage); e != nil {
		return e
	}
	receipt := Receipt{Version: Version, Platform: runtime.GOOS + "-" + runtime.GOARCH, Source: archiveURL, ArchiveSHA: archiveSHA, Files: files}
	b, e := json.MarshalIndent(receipt, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, "receipt.json"), append(b, '\n'), 0644); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = os.Rename(stage, root); e != nil {
		return e
	}
	fmt.Fprintf(out, "Installed verified SBF backend: %s\n", filepath.Join(root, "llvm"))
	return nil
}

type archiveWriter struct {
	w    io.Writer
	out  io.Writer
	n    int64
	last time.Time
}

func (p *archiveWriter) Write(b []byte) (int, error) {
	if int64(len(b)) > archiveSize-p.n {
		return 0, fmt.Errorf("download exceeds pinned archive size")
	}
	n, e := p.w.Write(b)
	p.n += int64(n)
	if time.Since(p.last) > 5*time.Second {
		fmt.Fprintf(p.out, "Downloaded %.0f / 448 MB\n", float64(p.n)/1e6)
		p.last = time.Now()
	}
	return n, e
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}

func extract(ctx context.Context, tr *tar.Reader, stage string) (map[string]string, error) {
	wanted := map[string]string{"llvm/bin/clang-19": "llvm/bin/clang", "llvm/bin/lld": "llvm/bin/ld.lld"}
	files := map[string]string{}
	for {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		n := strings.TrimPrefix(h.Name, "./")
		// Every output path comes from the allowlist, never directly from the archive.
		dest, ok := wanted[n]
		if !ok {
			continue
		}
		if path.Clean(n) != n || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > 400<<20 {
			return nil, fmt.Errorf("invalid backend archive member %q", h.Name)
		}
		if _, ok := files[dest]; ok {
			return nil, fmt.Errorf("duplicate backend archive member %q", h.Name)
		}
		p := filepath.Join(stage, filepath.FromSlash(dest))
		if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
		if e != nil {
			return nil, e
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(f, hash), contextReader{ctx, tr})
		closeErr := f.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		files[dest] = fmt.Sprintf("%x", hash.Sum(nil))
	}
	if len(files) != 2 {
		return nil, fmt.Errorf("archive missing Clang or LLD")
	}
	return files, nil
}

func smoke(ctx context.Context, stage string) error {
	dir, e := os.MkdirTemp("", "gosvm-toolchain-smoke-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	c := filepath.Join(dir, "test.c")
	obj := filepath.Join(dir, "test.o")
	elf := filepath.Join(dir, "test.so")
	if e = os.WriteFile(c, []byte("unsigned long long entrypoint(const unsigned char *p) { return p[0]; }\n"), 0600); e != nil {
		return e
	}
	for _, args := range [][]string{
		{filepath.Join(stage, "llvm/bin/clang"), "-target", "sbf", "-mcpu=v3", "-O2", "-fno-builtin", "-fPIC", "-c", c, "-o", obj},
		{filepath.Join(stage, "llvm/bin/ld.lld"), "-shared", "--no-undefined", "--entry", "entrypoint", obj, "-o", elf},
	} {
		b, e := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
		if e != nil {
			return fmt.Errorf("backend smoke test: %w\n%s", e, b)
		}
	}
	return nil
}
