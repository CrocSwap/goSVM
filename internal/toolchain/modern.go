package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	target "gosvm/internal/targets"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

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
func VerifyModern(root string) error {
	b, err := os.ReadFile(filepath.Join(root, "receipt.json"))
	if err != nil {
		return err
	}
	var r Receipt
	if err = json.Unmarshal(b, &r); err != nil {
		return err
	}
	if r.Version != target.ModernTools || r.Platform != runtime.GOOS+"-"+runtime.GOARCH || r.Source != "qualified-local-backend" || len(r.Files) != 2 {
		return fmt.Errorf("modern backend receipt mismatch")
	}
	for name, want := range target.ModernBackendHashes {
		if r.Files["llvm/bin/"+name] != want {
			return fmt.Errorf("modern backend content pin mismatch")
		}
	}
	p, _ := target.Resolve(target.Solana)
	return p.CheckBackend(filepath.Join(root, "llvm"))
}

// InstallModern imports only the two already qualified binaries. There is no
// unverified archive/download path for a candidate toolchain release.
func InstallModern(ctx context.Context, root, llvm string, out io.Writer) error {
	p, _ := target.Resolve(target.Solana)
	if err := p.CheckBackend(llvm); err != nil {
		return err
	}
	if _, err := os.Stat(root); err == nil {
		if err = VerifyModern(root); err != nil {
			return err
		}
		fmt.Fprintln(out, "Qualified backend already installed:", root)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(root), 0755); err != nil {
		return err
	}
	lock := root + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("modern installer busy: %w", err)
	}
	defer os.Remove(lock)
	stage, err := os.MkdirTemp(filepath.Dir(root), ".modern-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = os.MkdirAll(filepath.Join(stage, "llvm/bin"), 0755); err != nil {
		return err
	}
	files := map[string]string{}
	for name, hash := range target.ModernBackendHashes {
		if err = ctx.Err(); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(llvm, "bin", name))
		if err != nil {
			return err
		}
		dest := filepath.Join(stage, "llvm/bin", name)
		if err = os.WriteFile(dest, data, 0755); err != nil {
			return err
		}
		actual, err := hashFile(dest)
		if err != nil {
			return err
		}
		if actual != hash {
			return fmt.Errorf("backend changed during import")
		}
		files["llvm/bin/"+name] = hash
	}
	r := Receipt{Version: p.Tools, Platform: runtime.GOOS + "-" + runtime.GOARCH, Source: "qualified-local-backend", Files: files}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(stage, "receipt.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	if err = VerifyModern(stage); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(stage, root); err != nil {
		return err
	}
	fmt.Fprintln(out, "Installed content-pinned platform-tools v1.57 backend:", root)
	return nil
}
