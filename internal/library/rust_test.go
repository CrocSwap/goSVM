package library

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func requireRustTools(t *testing.T) string {
	t.Helper()
	root := os.Getenv("GOSVM_RUST_PLATFORM_TOOLS")
	if root == "" {
		root = DefaultPlatformTools()
	}
	if _, err := checkRustTools(root); err != nil {
		t.Skipf("pinned platform-tools v1.57 Rust toolchain unavailable: %v", err)
	}
	return root
}

func TestRustFingerprintAndWrapper(t *testing.T) {
	a := RustFingerprint("quote::quote", 16, 8, "")
	if a == RustFingerprint("quote::other", 16, 8, "") || a == RustFingerprint("quote::quote", 16, 9, "") || a == RustFingerprint("quote::quote", 16, 8, "v2") {
		t.Fatal("fingerprint ignores interface identity")
	}
	header := rustHeader(RustResult{Root: "quote::quote", Symbol: RustSymbolPrefix + a, Fingerprint: a, InputBytes: 16, OutputBytes: 8}, rustDecl{})
	normalized := strings.Join(strings.Fields(header), "")
	// The Bend consumer's header checks must accept the Rust header verbatim.
	for _, want := range []string{"#defineBEND_LIBRARY_ABI_VERSION1u", "#defineBEND_LIBRARY_INPUT_BYTES16u", "#defineBEND_LIBRARY_ABI_OUTPUT_ENCODING7u",
		"uint32_t" + RustSymbolPrefix + a + "(constuint8_t*input,uint64_tinput_len,uint8_t*output,uint64_toutput_capacity,uint64_t*output_len);"} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("header lacks %s", want)
		}
	}
}

func buildQuote(t *testing.T, tools string) (RustResult, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "quote-library")
	r, err := BuildRust(RustOptions{Crate: "testdata/rust/quote", Output: out, PlatformTools: tools})
	if err != nil {
		t.Fatal(err)
	}
	return r, out
}

func TestBuildRustLibrary(t *testing.T) {
	tools := requireRustTools(t)
	r, out := buildQuote(t, tools)
	if r.Root != "quote::quote" || r.Symbol != RustSymbolPrefix+r.Fingerprint || r.InputBytes != 16 || r.OutputBytes != 8 {
		t.Fatalf("unexpected interface %+v", r)
	}
	for profile, want := range map[string][2]uint32{"fogo-v0": {263, 0}, "solana-v3": {247, 3}} {
		data, err := os.ReadFile(r.Manifests[profile])
		if err != nil {
			t.Fatal(err)
		}
		var m RustProducer
		if err = decode(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Kind != RustKind || m.Target != profile || m.ABI != 1 || m.Authorized || m.Toolchains.Rust.PlatformTools != "v1.57" || m.Toolchains.Rust.Version != want[1] {
			t.Fatalf("%s manifest: %s", profile, data)
		}
		for name, a := range m.Artifacts {
			if _, err := (&Binding{Dir: filepath.Dir(r.Manifests[profile]), Metadata: map[string][]byte{}}).artifact(Artifact{name, a.SHA256}); err != nil {
				t.Fatal(err)
			}
		}
		sbf, _ := os.ReadFile(filepath.Join(out, profile, m.Selection["sbfObject"]))
		rt, _ := rustTargetFor(profile)
		imports, err := validateRustSBF(sbf, rt, r.Symbol)
		if err != nil || uint32(rt.machine) != want[0] {
			t.Fatal(profile, err)
		}
		// u128 multiply stays an import for GoSVM's single __multi3; u128
		// division is resolved from pinned compiler_builtins and made local.
		builtins := strings.Join(m.Toolchains.Rust.CompilerBuiltins.Symbols, ",")
		if strings.Join(imports, ",") != "__multi3" || strings.Join(m.Toolchains.Rust.Imports, ",") != "__multi3" || !strings.HasSuffix(builtins, "__ashlti3,__lshrti3,__udivti3") {
			t.Fatalf("%s imports %v builtins %s", profile, imports, builtins)
		}
	}
	// Reproducible across output locations.
	_, again := buildQuote(t, tools)
	for _, profile := range []string{"fogo-v0", "solana-v3"} {
		a, _ := os.ReadFile(filepath.Join(out, profile, "library.sbf.o"))
		b, _ := os.ReadFile(filepath.Join(again, profile, "library.sbf.o"))
		if !bytes.Equal(a, b) {
			t.Errorf("%s object differs between output directories", profile)
		}
	}
	if _, err := BuildRust(RustOptions{Crate: "testdata/rust/quote", Output: out, PlatformTools: tools}); err == nil {
		t.Fatal("overwrote an existing output")
	}
	if entries, _ := os.ReadDir("testdata/rust/quote"); len(entries) != 2 {
		t.Fatal("build wrote into the source crate")
	}
	t.Run("native cgo", func(t *testing.T) { nativeQuote(t, filepath.Join(out, "fogo-v0"), r) })
}

// nativeQuote links the host object through cgo in a throwaway module (cgo is
// unavailable in _test.go files) and exercises ABI v1 boundaries.
func nativeQuote(t *testing.T, dir string, r RustResult) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command unavailable")
	}
	module := t.TempDir()
	for _, name := range []string{"library.host.o", "library.h"} {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		os.WriteFile(filepath.Join(module, name), data, 0644)
	}
	os.WriteFile(filepath.Join(module, "go.mod"), []byte("module nativequote\n\ngo 1.22\n"), 0644)
	source := `package main
/*
#cgo LDFLAGS: ${SRCDIR}/library.host.o
#include "library.h"
static uint32_t call(const uint8_t *in, uint64_t n, uint8_t *out, uint64_t cap, uint64_t *len) { return GOSVM_LIBRARY_CALL(in, n, out, cap, len); }
*/
import "C"
import ("bytes";"fmt";"os";"unsafe")
func call(in, out []byte, length *uint64) uint32 {
	var ip, op *C.uint8_t
	if len(in) > 0 { ip = (*C.uint8_t)(unsafe.Pointer(&in[0])) }
	if len(out) > 0 { op = (*C.uint8_t)(unsafe.Pointer(&out[0])) }
	return uint32(C.call(ip, C.uint64_t(len(in)), op, C.uint64_t(len(out)), (*C.uint64_t)(unsafe.Pointer(length))))
}
func main() {
	fail := func(v ...any) { fmt.Println(v...); os.Exit(1) }
	in := []byte{100, 0, 0, 0, 0, 0, 0, 0, 0xe8, 3, 0, 0, 0xd0, 7, 0, 0} // 100, 1000, 2000
	out := make([]byte, 8)
	length := new(uint64)
	if s := call(in, out, length); s != 0 || *length != 8 || !bytes.Equal(out, []byte{181, 0, 0, 0, 0, 0, 0, 0}) { fail("quote", s, *length, out) }
	cases := []struct{ in, out []byte; status uint32 }{
		{[]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0}, make([]byte, 8), 4},
		{in[:15], make([]byte, 8), 4},
		{in, make([]byte, 7), 5},
		{in, nil, 1},
		{nil, make([]byte, 8), 4},
	}
	for _, c := range cases {
		*length = 99
		before := append([]byte(nil), c.out...)
		if s := call(c.in, c.out, length); s != c.status || *length != 99 || !bytes.Equal(before, c.out) { fail("case", c.status, s, *length) }
	}
	buf := append([]byte(nil), in...)
	if s := call(buf, buf[:8], length); s != 3 { fail("alias", s) }
	words := make([]uint64, 3)
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&words[0])), 24)
	if s := call(in, out, (*uint64)(unsafe.Pointer(&raw[1]))); s != 6 { fail("misaligned", s) }
	fmt.Println("native ABI ok")
}
`
	os.WriteFile(filepath.Join(module, "main.go"), []byte(source), 0644)
	args := []string{"run"}
	if runtime.GOOS == "darwin" {
		args = append(args, "-ldflags=-linkmode=external")
	}
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = module
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "native ABI ok") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestBuildRustRefusals(t *testing.T) {
	tools := requireRustTools(t)
	write := func(t *testing.T, cargo, lib string) string {
		dir := filepath.Join(t.TempDir(), "crate")
		os.MkdirAll(filepath.Join(dir, "src"), 0755)
		os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"bad\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"+cargo), 0644)
		os.WriteFile(filepath.Join(dir, "src", "lib.rs"), []byte(lib), 0644)
		return dir
	}
	meta := "[package.metadata.gosvm-library]\nfunction = \"f\"\ninput-bytes = 2\noutput-bytes = 1\n"
	for name, c := range map[string]struct{ cargo, lib, want string }{
		"no declaration":      {"", "#![no_std]\npub fn f(i: &[u8; 2]) -> [u8; 1] { [i[0]] }\n", "package.metadata.gosvm-library"},
		"std":                 {meta, "pub fn f(i: &[u8; 2]) -> [u8; 1] { [i[0]] }\n", "#![no_std]"},
		"width mismatch":      {meta, "#![no_std]\npub fn f(i: &[u8; 3]) -> [u8; 1] { [i[0]] }\n", "mismatched types"},
		"wrong output":        {meta, "#![no_std]\npub fn f(i: &[u8; 2]) -> [u8; 2] { *i }\n", "must return [u8; 1]"},
		"panic path":          {meta, "#![no_std]\npub fn f(i: &[u8; 2]) -> [u8; 1] { [i[0] / i[1]] }\n", "reachable panic path"},
		"mutable state":       {meta, "#![no_std]\nuse core::sync::atomic::{AtomicU8, Ordering};\nstatic N: AtomicU8 = AtomicU8::new(1);\npub fn f(i: &[u8; 2]) -> [u8; 1] { [N.fetch_add(i[0], Ordering::Relaxed)] }\n", "writable allocated section"},
		"registry dependency": {meta + "[dependencies]\nserde = \"1\"\n", "#![no_std]\npub fn f(i: &[u8; 2]) -> [u8; 1] { [i[0]] }\n", "offline"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := BuildRust(RustOptions{Crate: write(t, c.cargo, c.lib), Output: filepath.Join(t.TempDir(), "out"), PlatformTools: tools})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

// Without a Rust toolchain the producer names the managed install command, and
// the default root falls back to Solana's cache when nothing is managed.
func TestRustToolchainMissing(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("platform-tools v1.57 is qualified on macOS arm64 only")
	}
	t.Setenv("GOSVM_CACHE", t.TempDir())
	home, _ := os.UserHomeDir()
	if DefaultPlatformTools() != filepath.Join(home, ".cache/solana/v1.57/platform-tools") {
		t.Fatal(DefaultPlatformTools())
	}
	_, err := BuildRust(RustOptions{Crate: "testdata/rust/quote", Output: filepath.Join(t.TempDir(), "out"), PlatformTools: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "run: gosvm toolchain install -rust") {
		t.Fatalf("error lacks fix: %v", err)
	}
}
