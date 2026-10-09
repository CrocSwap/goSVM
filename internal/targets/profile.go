// Package target defines compiler profiles independently of application wire schemas.
package target

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const Legacy = "legacy-v1.51-v3"
const Solana = "solana-v3"
const Fogo = "fogo-v0"
const ModernTools = "v1.57"

type Profile struct {
	Name  string `json:"name"`
	Tools string `json:"platform_tools"`
	Arch  string `json:"sbf"`
	LLVM  string `json:"llvm_version"`
	Flags uint32 `json:"elf_flags"`
}

func Resolve(name string) (Profile, error) {
	switch name {
	case Legacy:
		return Profile{Legacy, "v1.51", "v3", "19.1.7", 3}, nil
	case Solana:
		return Profile{Solana, ModernTools, "v3", "22.1.2", 3}, nil
	case Fogo:
		return Profile{Fogo, ModernTools, "v0", "22.1.2", 0}, nil
	default:
		return Profile{}, fmt.Errorf("unknown target profile %q; choose fogo-v0, solana-v3, or %s", name, Legacy)
	}
}

// These identify the candidate backend qualified by the local acceptance corpus.
// Other host packages require their own qualification and pins.
var ModernBackendHashes = map[string]string{
	"clang":  "67ebfa7313086cf01ddb42d6cdf3b4bba741dc2341a1e70796d018fe7b9ee697",
	"ld.lld": "91db7cf05b55facfdb7178eb24d6e5eba1da369672f2b52ab1657165a6441630",
}

func (p Profile) CheckBackend(llvm string) error {
	for _, name := range []string{"clang", "ld.lld"} {
		path := filepath.Join(llvm, "bin", name)
		b, err := exec.Command(path, "--version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s requires platform-tools %s: %s: %w", p.Name, p.Tools, path, err)
		}
		if !strings.Contains(string(b), p.LLVM) {
			return fmt.Errorf("%s requires platform-tools %s / LLVM %s, got %s", p.Name, p.Tools, p.LLVM, b)
		}
		if p.Tools == ModernTools {
			if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
				return fmt.Errorf("platform-tools %s is currently qualified on macOS arm64 only", p.Tools)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if fmt.Sprintf("%x", sha256.Sum256(data)) != ModernBackendHashes[name] {
				return fmt.Errorf("%s backend %s does not match the qualified platform-tools %s content pin", p.Name, name, p.Tools)
			}
		}
	}
	return nil
}

func ValidateELF(data []byte, flags uint32, object bool) error {
	machine := uint16(263)
	if flags == 3 {
		machine = 247
	}
	return validateELF(data, flags, object, machine)
}

func (p Profile) ValidateProgram(data []byte) error {
	machine := uint16(263)
	if p.Name == Solana {
		machine = 247
	}
	return validateELF(data, p.Flags, false, machine)
}

func validateELF(data []byte, flags uint32, object bool, machine uint16) error {
	kind := uint16(3)
	if object {
		kind = 1
	}
	if len(data) < 64 || string(data[:4]) != "\x7fELF" || data[4] != 2 || data[5] != 1 || binary.LittleEndian.Uint16(data[16:18]) != kind || binary.LittleEndian.Uint16(data[18:20]) != machine || binary.LittleEndian.Uint32(data[48:52]) != flags {
		return fmt.Errorf("require a little-endian ELF64 SBF %s with version flags %d", map[bool]string{true: "object", false: "program"}[object], flags)
	}
	return nil
}
