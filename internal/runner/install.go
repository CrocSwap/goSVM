// Package runner manages explicitly installed, pinned experimental SVM runners.
// Each pinned package installs into its own version-specific cache slot.
package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"
)

// Historical runner: LiteSVM 0.8.2, legacy and fogo-v0 qualification.
const Version = "0.5.0"
const Identity = "gosvm-svm-runner 0.5.0 (LiteSVM 0.8.2 + rent-error patch)"

// Modern runner: LiteSVM 0.13.0 / Agave 4.0, required by solana-v3.
const ModernVersion = "0.6.0"
const ModernIdentity = "gosvm-svm-runner 0.6.0 (LiteSVM 0.13.0 + rent-error patch; modern profiles)"

// Pins are part of the frontend, never supplied by an archive or its sender.
// No public download endpoint is configured for these local experimental packages.
//
//go:embed pins/darwin-arm64.json
var pinned []byte

//go:embed pins/0.6.0-darwin-arm64.json
var modernPinned []byte

var identities = map[string]string{Version: Identity, ModernVersion: ModernIdentity}
var pins = map[string][]byte{Version: pinned, ModernVersion: modernPinned}

// Versions lists the pinned runner packages this frontend can install.
func Versions() []string { return []string{Version, ModernVersion} }

type File struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Pin struct {
	Schema       int             `json:"schema"`
	Version      string          `json:"version"`
	Identity     string          `json:"identity"`
	Platform     string          `json:"platform"`
	ArchiveName  string          `json:"archive_name"`
	ArchiveSHA   string          `json:"archive_sha256"`
	ArchiveBytes int64           `json:"archive_bytes"`
	Files        map[string]File `json:"files"`
}

// Package returns the historical 0.5.0 pin.
func Package() (Pin, error) { return PackageFor(Version) }

// PackageFor returns the frontend's embedded pin for a runner version.
func PackageFor(version string) (Pin, error) {
	var p Pin
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return p, fmt.Errorf("managed runner package is currently verified on macOS arm64 only; supply an explicit runner")
	}
	data, ok := pins[version]
	if !ok {
		return p, fmt.Errorf("no pinned runner package %q", version)
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.Schema != 1 || p.Version != version || p.Identity != identities[version] || p.Platform != runtime.GOOS+"-"+runtime.GOARCH || len(p.Files) == 0 {
		return p, fmt.Errorf("invalid embedded runner pin")
	}
	return p, nil
}

// InstallHint is the exact command that installs a pinned runner version.
func InstallHint(version string) string {
	name := "<pinned-package>"
	if p, err := PackageFor(version); err == nil {
		name = p.ArchiveName
	}
	return "gosvm runner install -archive " + name
}

// Root is the historical 0.5.0 slot.
func Root() string { return RootFor(Version) }

// RootFor is the version-specific managed slot.
func RootFor(version string) string {
	cache := os.Getenv("GOSVM_CACHE")
	if cache == "" {
		var err error
		cache, err = os.UserCacheDir()
		if err != nil {
			return ""
		}
		cache = filepath.Join(cache, "gosvm")
	}
	if !filepath.IsAbs(cache) {
		return ""
	}
	return filepath.Join(cache, "runners", version, runtime.GOOS+"-"+runtime.GOARCH)
}

func hashFile(file string) (string, error) {
	f, err := os.Open(file)
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

// Verify checks a historical 0.5.0 installation.
func Verify(root string) (Pin, error) { return VerifyVersion(Version, root) }

// VerifyVersion checks an installed root against the pinned package for version.
func VerifyVersion(version, root string) (Pin, error) {
	p, err := PackageFor(version)
	if err != nil {
		return p, err
	}
	return p, verify(root, p)
}

func verify(root string, p Pin) error {
	if root == "" {
		return fmt.Errorf("cannot resolve runner cache; set GOSVM_CACHE to an absolute path")
	}
	var receipt Pin
	b, err := os.ReadFile(filepath.Join(root, "receipt.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &receipt); err != nil {
		return err
	}
	if !reflect.DeepEqual(receipt, p) {
		return fmt.Errorf("managed runner receipt does not match the frontend's pinned package")
	}
	for name, expected := range p.Files {
		file := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != expected.Bytes {
			return fmt.Errorf("managed runner member %s is damaged", name)
		}
		if name == "bin/gosvm-svm-runner" && info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("managed runner is not executable")
		}
		h, err := hashFile(file)
		if err != nil {
			return err
		}
		if h != expected.SHA256 {
			return fmt.Errorf("managed runner member %s SHA-256 mismatch; restore the pinned package", name)
		}
	}
	return nil
}

// ManagedPath fails closed when a managed install exists but is damaged. An
// explicit runner remains an intentional override; normal tests never install.
func ManagedPath() (string, bool, error) { return ManagedPathFor(Version) }

// ManagedPathFor returns the verified managed runner of one version. It reports
// exists=false when that slot is not installed and an error when it is damaged.
func ManagedPathFor(version string) (string, bool, error) {
	root := RootFor(version)
	if root == "" {
		return "", false, fmt.Errorf("cannot resolve runner cache; set GOSVM_CACHE to an absolute path")
	}
	_, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	if _, err = VerifyVersion(version, root); err != nil {
		return "", true, fmt.Errorf("managed runner %s integrity check: %w; remove %s and run %s", version, err, root, InstallHint(version))
	}
	return filepath.Join(root, "bin/gosvm-svm-runner"), true, nil
}

// Install identifies which pinned package a local archive is (by size and
// SHA-256) and installs it into that version's slot.
func Install(ctx context.Context, archive string, out io.Writer) error {
	var known []Pin
	for _, v := range Versions() {
		p, err := PackageFor(v)
		if err != nil {
			return err
		}
		known = append(known, p)
	}
	if archive == "" {
		msg := "no public runner download is configured; install a pinned local package:"
		for _, p := range known {
			msg += fmt.Sprintf("\n  gosvm runner install -archive %s   (%s, SHA-256 %s)", p.ArchiveName, p.Identity, p.ArchiveSHA)
		}
		return fmt.Errorf("%s", msg)
	}
	p, err := identify(ctx, archive, known)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Recognized pinned package %s\n", p.Identity)
	return install(ctx, RootFor(p.Version), archive, out, p, smokeFor(p.Identity))
}

// identify hashes at most the largest pinned size; install re-verifies the
// private snapshot it extracts, so this choice cannot weaken verification.
func identify(ctx context.Context, archive string, known []Pin) (Pin, error) {
	var limit int64
	for _, p := range known {
		if p.ArchiveBytes > limit {
			limit = p.ArchiveBytes
		}
	}
	f, err := os.Open(archive)
	if err != nil {
		return Pin{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, &io.LimitedReader{R: contextReader{ctx, f}, N: limit + 1})
	if err != nil {
		return Pin{}, err
	}
	digest := fmt.Sprintf("%x", h.Sum(nil))
	for _, p := range known {
		if n == p.ArchiveBytes && digest == p.ArchiveSHA {
			return p, nil
		}
	}
	var names []string
	for _, p := range known {
		names = append(names, p.ArchiveName+" ("+p.ArchiveSHA+")")
	}
	return Pin{}, fmt.Errorf("runner archive size or SHA-256 mismatch: matches no pinned package (got %s); expected one of: %s", digest, strings.Join(names, ", "))
}

func install(ctx context.Context, root, archive string, out io.Writer, p Pin, check func(context.Context, string) error) error {
	if root == "" {
		return fmt.Errorf("cannot resolve runner cache; set GOSVM_CACHE to an absolute path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat(root); err == nil {
		if err = verify(root, p); err != nil {
			return err
		}
		fmt.Fprintf(out, "Runner already installed and verified: %s\n", root)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if archive == "" {
		return fmt.Errorf("no public runner download is configured; use gosvm runner install -archive %s (pinned SHA-256 %s)", p.ArchiveName, p.ArchiveSHA)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0755); err != nil {
		return err
	}
	lock := root + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("runner installer busy or interrupted; lock %s: %w", lock, err)
	}
	defer os.Remove(lock)
	tmp, err := os.MkdirTemp(filepath.Dir(root), ".runner-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// Snapshot the source into private staging. Hash and extract those same bytes;
	// changes to a supplied file cannot race the verified extraction.
	source, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer source.Close()
	copyPath := filepath.Join(tmp, "package.tar.gz")
	copyFile, err := os.OpenFile(copyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(copyFile, h), &io.LimitedReader{R: contextReader{ctx, source}, N: p.ArchiveBytes + 1})
	closeErr := copyFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n != p.ArchiveBytes || fmt.Sprintf("%x", h.Sum(nil)) != p.ArchiveSHA {
		return fmt.Errorf("runner archive size or SHA-256 mismatch")
	}
	f, err := os.Open(copyPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	gz.Multistream(false)
	stage := filepath.Join(tmp, "runner")
	if err = extract(ctx, gz, stage, p); err != nil {
		return err
	}
	if err = check(ctx, filepath.Join(stage, "bin/gosvm-svm-runner")); err != nil {
		return fmt.Errorf("runner smoke test: %w", err)
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(stage, "receipt.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	if err = verify(stage, p); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(stage, root); err != nil {
		return err
	}
	fmt.Fprintf(out, "Installed verified experimental runner %s: %s\n", p.Version, root)
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func safeMember(name string) bool {
	return name != "." && name != ".." && !strings.HasPrefix(name, "../") && !path.IsAbs(name) && path.Clean(name) == name && !strings.Contains(name, "\\")
}

func extract(ctx context.Context, input io.Reader, stage string, p Pin) error {
	var total int64
	for name, file := range p.Files {
		if !safeMember(name) || file.Bytes <= 0 || file.Bytes > 32<<20 {
			return fmt.Errorf("invalid pinned runner member %q", name)
		}
		total += file.Bytes
	}
	if total > 64<<20 {
		return fmt.Errorf("runner package exceeds extraction limit")
	}
	limited := &io.LimitedReader{R: contextReader{ctx, input}, N: total + 4<<20}
	tr := tar.NewReader(limited)
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		expected, ok := p.Files[h.Name]
		if !ok || !safeMember(h.Name) || h.Typeflag != tar.TypeReg || h.Size != expected.Bytes || seen[h.Name] {
			return fmt.Errorf("unexpected, unsafe or duplicate runner archive member %q", h.Name)
		}
		seen[h.Name] = true
		file := filepath.Join(stage, filepath.FromSlash(h.Name))
		if err = os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if h.Name == "bin/gosvm-svm-runner" {
			mode = 0755
		}
		f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(io.MultiWriter(f, hash), tr)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if fmt.Sprintf("%x", hash.Sum(nil)) != expected.SHA256 {
			return fmt.Errorf("runner member %s SHA-256 mismatch", h.Name)
		}
	}
	if len(seen) != len(p.Files) {
		return fmt.Errorf("runner archive is missing pinned members")
	}
	// Consume remaining padding to validate the gzip trailer within a bound.
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return err
	}
	if limited.N == 0 {
		return fmt.Errorf("runner archive exceeds decompression limit")
	}
	return nil
}

func smokeFor(identity string) func(context.Context, string) error {
	return func(ctx context.Context, binary string) error { return smoke(ctx, binary, identity) }
}

func smoke(ctx context.Context, binary, identity string) error {
	ctx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	version, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("version: %w: %s", err, version)
	}
	if strings.TrimSpace(string(version)) != identity {
		return fmt.Errorf("require %s; found %s", identity, version)
	}
	cmd := exec.CommandContext(ctx, binary, "--stdio")
	cmd.Stdin = strings.NewReader("{\"schema\":1,\"id\":0,\"op\":\"init\",\"config\":{\"programs\":[],\"accounts\":[],\"payer\":\"11111111111111111111111111111112\"}}\n")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stdio: %w: %s", err, output)
	}
	var reply struct {
		Schema int
		ID     *uint64
		Result struct{ Ready bool }
		Error  json.RawMessage
	}
	if err = json.Unmarshal(bytes.TrimSpace(output), &reply); err != nil {
		return err
	}
	if reply.Schema != 1 || reply.ID == nil || *reply.ID != 0 || !reply.Result.Ready || len(reply.Error) != 0 {
		return fmt.Errorf("runner stdio initialization failed: %s", output)
	}
	return nil
}
