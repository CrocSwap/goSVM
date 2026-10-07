package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type member struct {
	name, body string
	kind       byte
}

func testArchive(t *testing.T, members ...member) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tr := tar.NewWriter(gz)
	for _, m := range members {
		size := int64(len(m.body))
		if m.kind == tar.TypeSymlink || m.kind == tar.TypeLink {
			size = 0
		}
		if err := tr.WriteHeader(&tar.Header{Name: m.name, Size: size, Mode: 0755, Typeflag: m.kind, Linkname: "outside"}); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := io.WriteString(tr, m.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testPin(data []byte) Pin {
	body := []byte("pinned executable")
	return Pin{Schema: 1, Version: Version, Identity: Identity, Platform: "test", ArchiveSHA: fmt.Sprintf("%x", sha256.Sum256(data)), ArchiveBytes: int64(len(data)), Files: map[string]File{"bin/gosvm-svm-runner": {SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Bytes: int64(len(body))}}}
}

func testInstall(t *testing.T, data []byte, p Pin, ctx context.Context, check func(context.Context, string) error) (string, error) {
	t.Helper()
	dir := t.TempDir()
	archive := filepath.Join(dir, "package.tar.gz")
	if err := os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "installed")
	err := install(ctx, root, archive, io.Discard, p, check)
	if err != nil {
		if _, e := os.Stat(root); !os.IsNotExist(e) {
			t.Fatal("failure published a runner")
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.Name() != "package.tar.gz" && entry.Name() != "installed" {
			t.Fatalf("left temporary state: %s", entry.Name())
		}
	}
	return root, err
}

func TestAtomicInstallAndPinnedIntegrity(t *testing.T) {
	data := testArchive(t, member{"bin/gosvm-svm-runner", "pinned executable", tar.TypeReg})
	p := testPin(data)
	called := false
	root, err := testInstall(t, data, p, context.Background(), func(ctx context.Context, binary string) error {
		called = true
		if info, e := os.Stat(binary); e != nil || info.Mode().Perm() != 0755 {
			t.Fatal("binary permissions")
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("install: %v called=%v", err, called)
	}
	if err = verify(root, p); err != nil {
		t.Fatal(err)
	}
	if err = install(context.Background(), root, "", io.Discard, p, func(context.Context, string) error { t.Fatal("reinstalled existing runner"); return nil }); err != nil {
		t.Fatal(err)
	}
	// Changing both the file and receipt must not replace the embedded trust root.
	binary := filepath.Join(root, "bin/gosvm-svm-runner")
	if err = os.WriteFile(binary, []byte("edited executable"), 0755); err != nil {
		t.Fatal(err)
	}
	hash, _ := hashFile(binary)
	for name, f := range p.Files {
		f.SHA256 = hash
		p.Files[name] = f
	}
	b, _ := json.Marshal(p)
	if err = os.WriteFile(filepath.Join(root, "receipt.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	if err = verify(root, testPin(data)); err == nil {
		t.Fatal("mutable receipt replaced trusted pin")
	}
	if err = install(context.Background(), root, "", io.Discard, testPin(data), nil); err == nil {
		t.Fatal("damaged installation silently accepted")
	}
}

func TestRejectArchivesBeforeExecution(t *testing.T) {
	good := member{"bin/gosvm-svm-runner", "pinned executable", tar.TypeReg}
	for name, members := range map[string][]member{
		"traversal":       {{"../outside", "bad", tar.TypeReg}, good},
		"absolute":        {{"/outside", "bad", tar.TypeReg}, good},
		"duplicate":       {good, good},
		"symlink":         {{good.name, "", tar.TypeSymlink}},
		"hardlink":        {{good.name, "", tar.TypeLink}},
		"unknown":         {good, {"other", "bad", tar.TypeReg}},
		"missing":         {},
		"wrong-size":      {{good.name, "short", tar.TypeReg}},
		"wrong-file-hash": {{good.name, "edited executable", tar.TypeReg}},
	} {
		t.Run(name, func(t *testing.T) {
			data := testArchive(t, members...)
			_, err := testInstall(t, data, testPin(data), context.Background(), func(context.Context, string) error { t.Fatal("unsafe archive executed"); return nil })
			if err == nil {
				t.Fatal("accepted unsafe/incomplete archive")
			}
		})
	}
	data := testArchive(t, good)
	p := testPin(data)
	data[len(data)/2] ^= 1
	_, err := testInstall(t, data, p, context.Background(), func(context.Context, string) error { t.Fatal("unverified archive executed"); return nil })
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("corrupt archive: %v", err)
	}
}

func TestFailedSmokeAndCancellationDoNotPublish(t *testing.T) {
	data := testArchive(t, member{"bin/gosvm-svm-runner", "pinned executable", tar.TypeReg})
	_, err := testInstall(t, data, testPin(data), context.Background(), func(context.Context, string) error { return fmt.Errorf("incompatible host") })
	if err == nil || !strings.Contains(err.Error(), "smoke") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	_, err = testInstall(t, data, testPin(data), ctx, func(context.Context, string) error { cancel(); return nil })
	if err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestGzipTrailerAndInstalledFileType(t *testing.T) {
	data := testArchive(t, member{"bin/gosvm-svm-runner", "pinned executable", tar.TypeReg})
	corrupt := append([]byte(nil), data...)
	corrupt[len(corrupt)-8] ^= 1 // CRC, after an otherwise valid tar payload.
	_, err := testInstall(t, corrupt, testPin(corrupt), context.Background(), func(context.Context, string) error {
		t.Fatal("archive with invalid gzip trailer executed")
		return nil
	})
	if err == nil {
		t.Fatal("accepted invalid gzip CRC")
	}
	root, err := testInstall(t, data, testPin(data), context.Background(), func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "bin/gosvm-svm-runner")
	if err = os.Chmod(binary, 0644); err != nil {
		t.Fatal(err)
	}
	if err = verify(root, testPin(data)); err == nil {
		t.Fatal("accepted nonexecutable runner")
	}
	external := filepath.Join(t.TempDir(), "external")
	if err = os.WriteFile(external, []byte("pinned executable"), 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(external, binary); err != nil {
		t.Fatal(err)
	}
	if err = verify(root, testPin(data)); err == nil {
		t.Fatal("accepted installed symlink")
	}
}

func TestBusyInstallPreservesLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runner")
	if err := os.Mkdir(root+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	err := install(context.Background(), root, "unused", io.Discard, Pin{}, nil)
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatal(err)
	}
	if _, err = os.Stat(root + ".lock"); err != nil {
		t.Fatal("removed someone else's lock")
	}
}
