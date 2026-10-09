package library

// Rust producer for the pure-library ABI v1. A user writes a safe #![no_std]
// function over fixed byte arrays; GoSVM generates the unsafe C ABI wrapper,
// builds it with the pinned platform-tools Rust toolchain into one relocatable
// object per SBF profile plus a native host object, and publishes one producer
// manifest per profile in the same shape as Bend's library manifest.

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	target "gosvm/internal/targets"
	"gosvm/internal/toolchain"
)

const (
	RustKind         = "gosvm-rust-library-v1"
	RustSymbolPrefix = "gosvm_rust_v1_call_"
	// RustPanicSymbol is referenced only by the generated panic handler. If it
	// survives optimization, a panic path is reachable and the build is refused.
	RustPanicSymbol = "gosvm_rust_v1_library_panic_path_is_reachable"
)

// RustToolHashes pins the platform-tools v1.57 Rust compiler (macOS arm64);
// the map is shared with the managed toolchain installer.
var RustToolHashes = target.RustToolHashes

// RustTools is the platform-tools pin of GoSVM-built Rust library objects.
const RustTools = target.ModernTools

const rustCommit = "ae660768afd467d131776918a1627e18920b230c"

// The final GoSVM image supplies these helpers exactly once (memcpy/memset from
// the frontend runtime, memmove from the library runtime, __multi3 from the
// compiler builtins object). The Rust object may import nothing else.
var rustConsumerHelpers = map[string]bool{"memcpy": true, "memset": true, "memmove": true, "__multi3": true}

// Rust compiler_builtins memory functions call sol_mem* syscalls; never link them.
var rustForbiddenMembers = map[string]bool{"memcpy": true, "memset": true, "memmove": true, "memcmp": true, "bcmp": true}

type RustOptions struct {
	Crate         string   // directory containing Cargo.toml
	Output        string   // new directory; receives <profile>/library.manifest.json
	PlatformTools string   // platform-tools v1.57 root (default DefaultPlatformTools())
	Targets       []string // default fogo-v0 and solana-v3
}

type RustResult struct {
	Root        string
	Symbol      string
	Fingerprint string
	InputBytes  uint64
	OutputBytes uint64
	Manifests   map[string]string // profile -> manifest path
}

type rustDecl struct {
	Function    string `json:"function"`
	InputBytes  uint64 `json:"input-bytes"`
	OutputBytes uint64 `json:"output-bytes"`
	Schema      string `json:"schema,omitempty"`
}

type RustArtifact struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type RustToolchain struct {
	PlatformTools    string            `json:"platformTools"`
	Rustc            string            `json:"rustc"`
	Cargo            string            `json:"cargo"`
	LLVM             string            `json:"llvm"`
	Target           string            `json:"target"`
	CPU              string            `json:"cpu"`
	Version          uint32            `json:"sbpfVersion"`
	Profile          map[string]string `json:"profile"`
	Hashes           map[string]string `json:"sha256"`
	CompilerBuiltins struct {
		Archive string   `json:"archive"`
		SHA256  string   `json:"sha256"`
		Members []string `json:"members"`
		Symbols []string `json:"symbols"`
	} `json:"compilerBuiltins"`
	Imports []string `json:"imports"`
}

// RustProducer is the per-profile manifest. It mirrors Bend's library manifest
// fields; toolchains.rust replaces toolchains.anzaClang.
type RustProducer struct {
	Version     int                     `json:"version"`
	Kind        string                  `json:"kind"`
	Target      string                  `json:"targetProfile"`
	ABI         int                     `json:"abiVersion"`
	Crate       string                  `json:"crate"`
	Root        string                  `json:"root"`
	Symbol      string                  `json:"symbol"`
	Fingerprint string                  `json:"schemaFingerprint"`
	Input       uint64                  `json:"canonicalInputBytes"`
	Output      uint64                  `json:"canonicalOutputBytes"`
	Authorized  bool                    `json:"finalElfAuthorized"`
	Selection   map[string]string       `json:"selection"`
	Artifacts   map[string]RustArtifact `json:"artifacts"`
	Inputs      map[string]string       `json:"inputs"`
	Toolchains  struct {
		Rust RustToolchain `json:"rust"`
		Host struct {
			Rustc  string `json:"rustc"`
			Target string `json:"target"`
			GOOS   string `json:"goos"`
			GOARCH string `json:"goarch"`
		} `json:"hostRust"`
	} `json:"toolchains"`
}

var rustPathRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(::[A-Za-z_][A-Za-z0-9_]*)*$`)

// RustFingerprint is the interface identity: ABI, root, widths and the optional
// declared canonical schema text. It does not depend on implementation bytes.
func RustFingerprint(root string, input, output uint64, schema string) string {
	canonical, _ := json.Marshal(struct {
		Kind   string `json:"kind"`
		ABI    int    `json:"abiVersion"`
		Root   string `json:"root"`
		Input  uint64 `json:"inputBytes"`
		Output uint64 `json:"outputBytes"`
		Schema string `json:"schema"`
	}{RustKind, 1, root, input, output, schema})
	return fmt.Sprintf("%x", sha256.Sum256(canonical))
}

// DefaultPlatformTools prefers the managed full tree installed by
// "gosvm toolchain install -rust", else Solana's v1.57 platform-tools cache.
func DefaultPlatformTools() string {
	if p := toolchain.RustPlatformTools(); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "solana", target.ModernTools, "platform-tools")
}

type rustTools struct {
	root, rustc, cargo, llvm string
	version, cargoVersion    string
	hashes                   map[string]string
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// RustInstallCommand installs the managed Rust producer toolchain.
const RustInstallCommand = "gosvm toolchain install -rust"

// CheckRustTools verifies a platform-tools root for the Rust producer (content
// pins and rustc identity) without building anything.
func CheckRustTools(root string) error {
	_, err := checkRustTools(root)
	return err
}

func checkRustTools(root string) (rustTools, error) {
	t := rustTools{root: root, rustc: filepath.Join(root, "rust", "bin", "rustc"), cargo: filepath.Join(root, "rust", "bin", "cargo"), llvm: filepath.Join(root, "llvm"), hashes: map[string]string{}}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return t, fmt.Errorf("Rust library producer: platform-tools %s is qualified on macOS arm64 only", target.ModernTools)
	}
	drivers, _ := filepath.Glob(filepath.Join(root, "rust", "lib", "librustc_driver-*.dylib"))
	if len(drivers) != 1 {
		return t, fmt.Errorf("Rust library producer: platform-tools %s Rust compiler not found under %s; run: %s", target.ModernTools, root, RustInstallCommand)
	}
	paths := map[string]string{"rustc": t.rustc, "cargo": t.cargo, "librustc_driver": drivers[0], "llvm-objcopy": filepath.Join(t.llvm, "bin", "llvm-objcopy"), "llvm-ar": filepath.Join(t.llvm, "bin", "llvm-ar")}
	for name, path := range paths {
		h, err := fileHash(path)
		if err != nil {
			return t, fmt.Errorf("Rust library producer requires platform-tools %s: %w; run: %s", target.ModernTools, err, RustInstallCommand)
		}
		if h != RustToolHashes[name] {
			return t, fmt.Errorf("Rust library producer: %s does not match the platform-tools %s content pin; run: %s", path, target.ModernTools, RustInstallCommand)
		}
		t.hashes[name] = h
	}
	// Clang/LLD are the same content-pinned backend the Go compiler qualifies.
	p, _ := target.Resolve(target.Solana)
	if err := p.CheckBackend(t.llvm); err != nil {
		return t, err
	}
	t.hashes["ld.lld"] = target.ModernBackendHashes["ld.lld"]
	out, err := exec.Command(t.rustc, "-vV").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "commit-hash: "+rustCommit) || !strings.Contains(string(out), "LLVM version: 22.1.2") {
		return t, fmt.Errorf("Rust library producer requires platform-tools %s rustc %s: %s", target.ModernTools, rustCommit, out)
	}
	t.version = strings.SplitN(string(out), "\n", 2)[0]
	out, err = exec.Command(t.cargo, "-V").CombinedOutput()
	if err != nil {
		return t, fmt.Errorf("cargo: %w: %s", err, out)
	}
	t.cargoVersion = strings.TrimSpace(string(out))
	return t, nil
}

type rustTarget struct {
	triple, cpu string
	sbpf        uint32
	machine     elf.Machine
}

func rustTargetFor(profile string) (rustTarget, error) {
	switch profile {
	case target.Fogo:
		return rustTarget{"sbpf-solana-solana", "generic", 0, 263}, nil
	case target.Solana:
		return rustTarget{"sbpfv3-solana-solana", "v3", 3, 247}, nil
	}
	return rustTarget{}, fmt.Errorf("Rust libraries support fogo-v0 and solana-v3, not %q", profile)
}

// BuildRust builds a pure Rust library crate into ABI v1 artifacts. Output must
// not exist; it is published atomically with one manifest per profile.
func BuildRust(o RustOptions) (RustResult, error) {
	var r RustResult
	if o.PlatformTools == "" {
		o.PlatformTools = DefaultPlatformTools()
	}
	if len(o.Targets) == 0 {
		o.Targets = []string{target.Fogo, target.Solana}
	}
	crate, err := filepath.Abs(o.Crate)
	if err != nil {
		return r, err
	}
	output, err := filepath.Abs(o.Output)
	if err != nil {
		return r, err
	}
	if _, err = os.Lstat(output); err == nil {
		return r, fmt.Errorf("%s already exists", output)
	}
	tools, err := checkRustTools(o.PlatformTools)
	if err != nil {
		return r, err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return r, err
	}
	work, err := os.MkdirTemp(filepath.Dir(output), ".gosvm-rust-work-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(work)
	// Isolate from the user's Cargo/rustup configuration and registry cache.
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CARGO") && !strings.HasPrefix(kv, "RUST") {
			env = append(env, kv)
		}
	}
	env = append(env, "CARGO_HOME="+filepath.Join(work, "cargo-home"), "RUSTC="+tools.rustc, "CARGO_NET_OFFLINE=true",
		"CARGO_ENCODED_RUSTFLAGS="+strings.Join([]string{"--remap-path-prefix=" + crate + "=/gosvm/crate", "--remap-path-prefix=" + work + "=/gosvm/work"}, "\x1f"))
	cargo := func(dir string, args ...string) ([]byte, error) {
		cmd := exec.Command(tools.cargo, args...)
		cmd.Dir, cmd.Env = dir, env
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("cargo %s: %w\n%s", args[0], err, stderr.String())
		}
		return stdout.Bytes(), nil
	}

	// Declaration: [package.metadata.gosvm-library] in Cargo.toml.
	meta, err := cargo(crate, "metadata", "--format-version", "1", "--no-deps", "--offline", "--manifest-path", filepath.Join(crate, "Cargo.toml"))
	if err != nil {
		return r, err
	}
	var metadata struct {
		Packages []struct {
			Name         string `json:"name"`
			ManifestPath string `json:"manifest_path"`
			Targets      []struct {
				Name    string   `json:"name"`
				Kind    []string `json:"kind"`
				SrcPath string   `json:"src_path"`
			} `json:"targets"`
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"packages"`
	}
	if err = json.Unmarshal(meta, &metadata); err != nil {
		return r, err
	}
	var pkgIndex = -1
	for i, p := range metadata.Packages {
		if filepath.Clean(p.ManifestPath) == filepath.Join(crate, "Cargo.toml") {
			pkgIndex = i
		}
	}
	if pkgIndex < 0 {
		return r, fmt.Errorf("%s is not a Cargo package", crate)
	}
	pkg := metadata.Packages[pkgIndex]
	rawDecl, ok := pkg.Metadata["gosvm-library"]
	if !ok {
		return r, fmt.Errorf("%s: add [package.metadata.gosvm-library] with function, input-bytes and output-bytes", pkg.ManifestPath)
	}
	var decl rustDecl
	if err = decode(rawDecl, &decl); err != nil {
		return r, fmt.Errorf("[package.metadata.gosvm-library]: %w", err)
	}
	if !rustPathRE.MatchString(decl.Function) || decl.InputBytes == 0 || decl.OutputBytes == 0 || decl.InputBytes > 1024 || decl.OutputBytes > 1024 {
		return r, fmt.Errorf("[package.metadata.gosvm-library] requires a Rust function path and input-bytes/output-bytes in 1..1024")
	}
	libName, libSource := "", ""
	for _, t := range pkg.Targets {
		for _, k := range t.Kind {
			if k == "lib" || k == "rlib" {
				libName, libSource = t.Name, t.SrcPath
			}
		}
	}
	if libName == "" {
		return r, fmt.Errorf("%s has no library target", pkg.Name)
	}
	libName = strings.ReplaceAll(libName, "-", "_")
	source, err := os.ReadFile(libSource)
	if err != nil {
		return r, err
	}
	if !regexp.MustCompile(`(?m)^\s*#!\[no_std\]`).Match(source) {
		return r, fmt.Errorf("%s: a GoSVM Rust library must be #![no_std]", libSource)
	}
	r.Root = libName + "::" + decl.Function
	r.InputBytes, r.OutputBytes = decl.InputBytes, decl.OutputBytes
	r.Fingerprint = RustFingerprint(r.Root, decl.InputBytes, decl.OutputBytes, decl.Schema)
	r.Symbol = RustSymbolPrefix + r.Fingerprint
	inputs, err := hashTree(crate)
	if err != nil {
		return r, err
	}

	wrapper := filepath.Join(work, "wrapper")
	if err = os.MkdirAll(filepath.Join(wrapper, "src"), 0755); err != nil {
		return r, err
	}
	files := map[string][]byte{
		"Cargo.toml": []byte(rustWrapperManifest(pkg.Name, crate)),
		"src/lib.rs": []byte(rustWrapperSource(libName, decl, r.Symbol)),
	}
	for name, data := range files {
		if err = os.WriteFile(filepath.Join(wrapper, name), data, 0644); err != nil {
			return r, err
		}
	}
	targetDir := filepath.Join(work, "target")
	objects := filepath.Join(work, "objects")
	if err = os.MkdirAll(objects, 0755); err != nil {
		return r, err
	}
	build := func(triple, out string) error {
		args := []string{"rustc", "--offline", "--release", "--lib", "--target-dir", targetDir}
		if triple != "" {
			args = append(args, "--target", triple)
		}
		args = append(args, "--", "--emit=obj="+out)
		_, err := cargo(wrapper, args...)
		return err
	}

	// Native host object (same pinned rustc, host sysroot).
	host := filepath.Join(objects, "library.host.o")
	if err = build("", host); err != nil {
		return r, err
	}
	if err = validateRustHost(host, r.Symbol); err != nil {
		return r, err
	}
	header := []byte(rustHeader(r, decl))
	// Field names match Bend's library.schema.json; "schema" is the declared text.
	var schemaBuf bytes.Buffer
	enc := json.NewEncoder(&schemaBuf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err = enc.Encode(map[string]any{"version": 1, "abiVersion": 1, "root": r.Root, "symbol": r.Symbol, "schemaFingerprint": r.Fingerprint, "inputBytes": r.InputBytes, "outputBytes": r.OutputBytes, "schema": decl.Schema, "producer": RustKind}); err != nil {
		return r, err
	}
	schema := schemaBuf.Bytes()
	hostData, err := os.ReadFile(host)
	if err != nil {
		return r, err
	}

	stage, err := os.MkdirTemp(filepath.Dir(output), ".gosvm-rust-out-")
	if err != nil {
		return r, err
	}
	defer os.RemoveAll(stage)
	r.Manifests = map[string]string{}
	for _, profile := range o.Targets {
		rt, err := rustTargetFor(profile)
		if err != nil {
			return r, err
		}
		raw := filepath.Join(objects, profile+".lto.o")
		if err = build(rt.triple, raw); err != nil {
			return r, err
		}
		final := filepath.Join(objects, profile+".sbf.o")
		tc, err := linkRustBuiltins(tools, rt, raw, final, filepath.Join(work, "builtins-"+profile), r.Symbol)
		if err != nil {
			return r, fmt.Errorf("%s: %w", profile, err)
		}
		sbf, err := os.ReadFile(final)
		if err != nil {
			return r, err
		}
		if tc.Imports, err = validateRustSBF(sbf, rt, r.Symbol); err != nil {
			return r, fmt.Errorf("%s: %w", profile, err)
		}
		tc.PlatformTools, tc.Rustc, tc.Cargo, tc.LLVM, tc.Target, tc.CPU, tc.Version = target.ModernTools, tools.version, tools.cargoVersion, "22.1.2", rt.triple, rt.cpu, rt.sbpf
		tc.Profile = map[string]string{"opt-level": "3", "codegen-units": "1", "lto": "fat", "panic": "abort", "overflow-checks": "false", "debug": "false"}
		tc.Hashes = tools.hashes
		dir := filepath.Join(stage, profile)
		if err = os.MkdirAll(dir, 0755); err != nil {
			return r, err
		}
		m := RustProducer{Version: 1, Kind: RustKind, Target: profile, ABI: 1, Crate: pkg.Name, Root: r.Root, Symbol: r.Symbol, Fingerprint: r.Fingerprint, Input: r.InputBytes, Output: r.OutputBytes,
			Selection: map[string]string{"hostObject": "library.host.o", "sbfObject": "library.sbf.o", "header": "library.h", "schema": "library.schema.json"},
			Artifacts: map[string]RustArtifact{}, Inputs: inputs}
		m.Toolchains.Rust = tc
		m.Toolchains.Host.Rustc, m.Toolchains.Host.Target, m.Toolchains.Host.GOOS, m.Toolchains.Host.GOARCH = tools.version, "aarch64-apple-darwin", runtime.GOOS, runtime.GOARCH
		for name, data := range map[string][]byte{"library.host.o": hostData, "library.sbf.o": sbf, "library.h": header, "library.schema.json": schema} {
			if err = os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
				return r, err
			}
			m.Artifacts[name] = RustArtifact{int64(len(data)), fmt.Sprintf("%x", sha256.Sum256(data))}
		}
		data, _ := json.MarshalIndent(m, "", "  ")
		// The manifest is written last, as in Bend's producer.
		if err = os.WriteFile(filepath.Join(dir, "library.manifest.json"), append(data, '\n'), 0644); err != nil {
			return r, err
		}
		r.Manifests[profile] = filepath.Join(output, profile, "library.manifest.json")
	}
	if err = os.Rename(stage, output); err != nil {
		return r, err
	}
	return r, nil
}

func hashTree(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel != "." && (d.Name() == "target" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		h, err := fileHash(path)
		out[filepath.ToSlash(rel)] = h
		return err
	})
	return out, err
}

func rustWrapperManifest(name, crate string) string {
	path, _ := json.Marshal(crate)
	return fmt.Sprintf(`# Generated by GoSVM. Do not edit.
[package]
name = "gosvm_rust_abi"
version = "0.0.0"
edition = "2021"
publish = false

[lib]
path = "src/lib.rs"
crate-type = ["staticlib"]

[dependencies]
user = { package = %q, path = %s }

[profile.release]
opt-level = 3
codegen-units = 1
lto = "fat"
panic = "abort"
debug = false
overflow-checks = false
incremental = false

[workspace]
`, name, path)
}

// The generated wrapper performs the ABI v1 checks in Bend's order: null use,
// pointer-range overflow, output_len alignment, aliasing, exact input length,
// canonical decoding (the user function's Err), output capacity, then publish.
func rustWrapperSource(lib string, d rustDecl, symbol string) string {
	return fmt.Sprintf(`// Generated by GoSVM. Do not edit.
#![no_std]
#![deny(unsafe_op_in_unsafe_fn)]

const INPUT_BYTES: usize = %[1]d;
const OUTPUT_BYTES: usize = %[2]d;

#[diagnostic::on_unimplemented(message = "a GoSVM Rust library function must return [u8; %[2]d] or Result<[u8; %[2]d], E> (declared output-bytes)")]
trait LibraryOutput { fn into_bytes(self) -> Option<[u8; OUTPUT_BYTES]>; }
impl LibraryOutput for [u8; OUTPUT_BYTES] { #[inline(always)] fn into_bytes(self) -> Option<[u8; OUTPUT_BYTES]> { Some(self) } }
impl<E> LibraryOutput for Result<[u8; OUTPUT_BYTES], E> { #[inline(always)] fn into_bytes(self) -> Option<[u8; OUTPUT_BYTES]> { self.ok() } }

#[inline(always)]
fn overlaps(a: u64, an: u64, b: u64, bn: u64) -> bool { an != 0 && bn != 0 && a < b + bn && b < a + an }

/// # Safety
/// ABI v1 caller preconditions: readable input, writable output and output_len.
#[no_mangle]
pub unsafe extern "C" fn %[3]s(input: *const u8, input_len: u64, output: *mut u8, output_capacity: u64, output_len: *mut u64) -> u32 {
    if output.is_null() || output_len.is_null() || (input.is_null() && input_len != 0) { return 1; }
    let (i, o, l) = (input as usize as u64, output as usize as u64, output_len as usize as u64);
    if i.checked_add(input_len).is_none() || o.checked_add(output_capacity).is_none() || l.checked_add(8).is_none() { return 2; }
    if l %% 8 != 0 { return 6; }
    if overlaps(i, input_len, o, output_capacity) || overlaps(i, input_len, l, 8) || overlaps(o, output_capacity, l, 8) { return 3; }
    if input_len != INPUT_BYTES as u64 { return 4; }
    let mut bytes = [0u8; INPUT_BYTES];
    // SAFETY: input is non-null, non-wrapping and exactly INPUT_BYTES long.
    unsafe { core::ptr::copy_nonoverlapping(input, bytes.as_mut_ptr(), INPUT_BYTES) };
    let root: fn(&[u8; INPUT_BYTES]) -> _ = user::%[4]s;
    let Some(result) = LibraryOutput::into_bytes(root(&bytes)) else { return 4 };
    if output_capacity < OUTPUT_BYTES as u64 { return 5; }
    // SAFETY: output has OUTPUT_BYTES capacity and does not overlap output_len.
    unsafe {
        core::ptr::copy_nonoverlapping(result.as_ptr(), output, OUTPUT_BYTES);
        core::ptr::write(output_len, OUTPUT_BYTES as u64);
    }
    0
}

#[cfg(target_os = "solana")]
#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! {
    // Never defined: a surviving reference means a reachable panic, which
    // GoSVM refuses instead of linking panic machinery or an abort syscall.
    extern "C" { fn %[5]s() -> !; }
    unsafe { %[5]s() }
}

#[cfg(not(target_os = "solana"))]
#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! {
    extern "C" { fn abort() -> !; }
    unsafe { abort() }
}
`, d.InputBytes, d.OutputBytes, symbol, d.Function, RustPanicSymbol)
}

// Header macro names match Bend's generated header so one consumer check
// accepts both producers; GOSVM_LIBRARY_* are neutral aliases.
func rustHeader(r RustResult, d rustDecl) string {
	var b strings.Builder
	fmt.Fprintf(&b, "/* Stable call boundary for a GoSVM Rust library (%s); canonical bytes carry the root schema. */\n#pragma once\n#include <stdint.h>\n\n", r.Root)
	for _, prefix := range []string{"BEND_LIBRARY", "GOSVM_LIBRARY"} {
		fmt.Fprintf(&b, "#define %s_ABI_VERSION 1u\n#define %s_SCHEMA_FINGERPRINT \"%s\"\n#define %s_INPUT_BYTES %du\n#define %s_OUTPUT_BYTES %du\n#define %s_CALL %s\n", prefix, prefix, r.Fingerprint, prefix, r.InputBytes, prefix, r.OutputBytes, prefix, r.Symbol)
		for i, name := range []string{"OK", "NULL", "RANGE", "ALIAS", "MALFORMED_INPUT", "SHORT_OUTPUT", "MISALIGNED_OUTPUT_LEN", "OUTPUT_ENCODING"} {
			fmt.Fprintf(&b, "#define %s_ABI_%s %du\n", prefix, name, i)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "/* Input is the fixed-width canonical argument encoding. */\nuint32_t %s(const uint8_t *input, uint64_t input_len,\n    uint8_t *output, uint64_t output_capacity, uint64_t *output_len);\n", r.Symbol)
	return b.String()
}

type elfSymbols struct {
	defined   map[string]bool
	undefined map[string]bool
}

func readSymbols(path string) (elfSymbols, error) {
	s := elfSymbols{map[string]bool{}, map[string]bool{}}
	f, err := elf.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	symbols, err := f.Symbols()
	if err != nil && err != elf.ErrNoSymbols {
		return s, err
	}
	for _, sym := range symbols {
		bind := elf.ST_BIND(sym.Info)
		if sym.Name == "" || (bind != elf.STB_GLOBAL && bind != elf.STB_WEAK) {
			continue
		}
		if sym.Section == elf.SHN_UNDEF {
			s.undefined[sym.Name] = true
		} else {
			s.defined[sym.Name] = true
		}
	}
	return s, nil
}

// linkRustBuiltins resolves the LTO object's compiler libcalls (for example
// __udivti3) against the pinned sysroot's compiler_builtins members, merges
// them into one relocatable object and makes every helper local. Memory helpers
// and __multi3 remain imports for the final image's single providers.
func linkRustBuiltins(t rustTools, rt rustTarget, raw, final, scratch, symbol string) (RustToolchain, error) {
	var tc RustToolchain
	tc.CompilerBuiltins.Members, tc.CompilerBuiltins.Symbols = []string{}, []string{}
	archives, _ := filepath.Glob(filepath.Join(t.root, "rust", "lib", "rustlib", rt.triple, "lib", "libcompiler_builtins-*.rlib"))
	if len(archives) != 1 {
		return tc, fmt.Errorf("pinned sysroot lacks compiler_builtins for %s", rt.triple)
	}
	tc.CompilerBuiltins.Archive = filepath.Base(archives[0])
	h, err := fileHash(archives[0])
	if err != nil {
		return tc, err
	}
	tc.CompilerBuiltins.SHA256 = h
	root, err := readSymbols(raw)
	if err != nil {
		return tc, err
	}
	if root.undefined[RustPanicSymbol] {
		return tc, fmt.Errorf("the Rust library has a reachable panic path (indexing, unwrap, division by zero, ...); return Err for malformed input and use checked/fixed-size operations so release optimization removes every panic")
	}
	if err = os.MkdirAll(scratch, 0755); err != nil {
		return tc, err
	}
	need := []string{}
	for name := range root.undefined {
		if !rustConsumerHelpers[name] {
			need = append(need, name)
		}
	}
	sort.Strings(need)
	link := []string{raw}
	if len(need) > 0 {
		ar := exec.Command(filepath.Join(t.llvm, "bin", "llvm-ar"), "x", archives[0])
		ar.Dir = scratch
		if out, err := ar.CombinedOutput(); err != nil {
			return tc, fmt.Errorf("extract compiler_builtins: %w: %s", err, out)
		}
		members, _ := filepath.Glob(filepath.Join(scratch, "*.o"))
		sort.Strings(members)
		provider := map[string]string{}
		info := map[string]elfSymbols{}
		for _, m := range members {
			s, err := readSymbols(m)
			if err != nil {
				return tc, err
			}
			info[m] = s
			for name := range s.defined {
				if provider[name] == "" {
					provider[name] = m
				}
			}
		}
		selected, resolved := map[string]bool{}, map[string]bool{}
		for len(need) > 0 {
			name := need[0]
			need = need[1:]
			if resolved[name] || rustConsumerHelpers[name] {
				continue
			}
			resolved[name] = true
			m := provider[name]
			if name == "memcmp" || name == "bcmp" {
				return tc, fmt.Errorf("unsupported import %s (large slice/array comparison); GoSVM supplies only memcpy/memset/memmove, compare with an explicit loop", name)
			}
			if m == "" || strings.HasPrefix(name, "sol_") || name == "abort" {
				return tc, fmt.Errorf("unsupported import %s: GoSVM Rust libraries may not use syscalls, allocation, std or unresolvable helpers", name)
			}
			if selected[m] {
				continue
			}
			for def := range info[m].defined {
				if rustForbiddenMembers[def] {
					return tc, fmt.Errorf("helper %s would link compiler_builtins memory functions that use syscalls", name)
				}
			}
			selected[m] = true
			tc.CompilerBuiltins.Members = append(tc.CompilerBuiltins.Members, filepath.Base(m))
			tc.CompilerBuiltins.Symbols = append(tc.CompilerBuiltins.Symbols, name)
			var more []string
			for u := range info[m].undefined {
				more = append(more, u)
			}
			sort.Strings(more)
			need = append(need, more...)
		}
		// Deterministic link order: the members are named by CGU index.
		sort.Strings(tc.CompilerBuiltins.Members)
		sort.Strings(tc.CompilerBuiltins.Symbols)
		for _, m := range tc.CompilerBuiltins.Members {
			link = append(link, filepath.Join(scratch, m))
		}
	}
	merged := final + ".merged"
	if out, err := exec.Command(filepath.Join(t.llvm, "bin", "ld.lld"), append([]string{"-r", "-o", merged}, link...)...).CombinedOutput(); err != nil {
		return tc, fmt.Errorf("ld.lld -r: %w: %s", err, out)
	}
	// Helpers become file-local: the object exports exactly the ABI symbol and
	// cannot interpose on (or be interposed by) another library's helpers.
	if out, err := exec.Command(filepath.Join(t.llvm, "bin", "llvm-objcopy"), "--localize-hidden", "--strip-unneeded", "--remove-section=.comment", "--remove-section=.llvmbc", "--remove-section=.llvmcmd", merged, final).CombinedOutput(); err != nil {
		return tc, fmt.Errorf("llvm-objcopy: %w: %s", err, out)
	}
	// The ABI symbol stays global (linkable) but hidden, so the final program
	// does not re-export it in .dynsym.
	if out, err := exec.Command(filepath.Join(t.llvm, "bin", "llvm-objcopy"), "--set-symbol-visibility="+symbol+"=hidden", final).CombinedOutput(); err != nil {
		return tc, fmt.Errorf("llvm-objcopy: %w: %s", err, out)
	}
	return tc, nil
}

// validateRustSBF checks the published object: profile header, exactly one
// global definition (the ABI function), imports limited to the final image's
// helper providers, and no writable allocated data (discarded by the v3 link).
func validateRustSBF(data []byte, rt rustTarget, symbol string) ([]string, error) {
	if err := target.ValidateELF(data, rt.sbpf, true); err != nil {
		return nil, err
	}
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if f.Machine != rt.machine {
		return nil, fmt.Errorf("object machine %d, want %d", f.Machine, rt.machine)
	}
	for _, s := range f.Sections {
		if s.Flags&elf.SHF_ALLOC != 0 && s.Flags&elf.SHF_WRITE != 0 && s.Size > 0 {
			return nil, fmt.Errorf("object has writable allocated section %s; library state, statics with pointers and mutable globals are unsupported", s.Name)
		}
		if s.Flags&elf.SHF_TLS != 0 {
			return nil, fmt.Errorf("thread-local section %s unsupported", s.Name)
		}
	}
	symbols, err := f.Symbols()
	if err != nil {
		return nil, err
	}
	var exports, imports []string
	for _, s := range symbols {
		bind := elf.ST_BIND(s.Info)
		if s.Name == "" || (bind != elf.STB_GLOBAL && bind != elf.STB_WEAK) {
			continue
		}
		if s.Section == elf.SHN_UNDEF {
			if !rustConsumerHelpers[s.Name] {
				return nil, fmt.Errorf("unresolved import %s", s.Name)
			}
			imports = append(imports, s.Name)
			continue
		}
		if s.Name != symbol || bind != elf.STB_GLOBAL || elf.ST_TYPE(s.Info) != elf.STT_FUNC || elf.ST_VISIBILITY(s.Other) != elf.STV_HIDDEN {
			return nil, fmt.Errorf("unexpected exported symbol %s", s.Name)
		}
		exports = append(exports, s.Name)
	}
	if len(exports) != 1 {
		return nil, fmt.Errorf("object must export exactly %s", symbol)
	}
	sort.Strings(imports)
	if imports == nil {
		imports = []string{}
	}
	return imports, nil
}

func validateRustHost(path, symbol string) error {
	if err := validateHost(path); err != nil {
		return err
	}
	want := symbol
	if runtime.GOOS == "darwin" {
		want = "_" + symbol
		f, err := macho.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if f.Symtab != nil {
			for _, s := range f.Symtab.Syms {
				if s.Name == want && s.Type&0x0e == 0x0e && s.Type&0x01 != 0 { // N_SECT | N_EXT
					return nil
				}
			}
		}
		return fmt.Errorf("host object does not export %s", symbol)
	}
	s, err := readSymbols(path)
	if err != nil {
		return err
	}
	if !s.defined[want] {
		return fmt.Errorf("host object does not export %s", symbol)
	}
	return nil
}

// RustBuildKey identifies a crate's source tree, target set and the producer's
// pinned compiler, so an unchanged crate reuses its previous build.
func RustBuildKey(crate string, targets []string) (string, error) {
	inputs, err := hashTree(crate)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)
	h := sha256.New()
	fmt.Fprintf(h, "gosvm-rust-build-v1\x00%s\x00%s\x00%s\n", RustTools, rustCommit, strings.Join(sorted, ","))
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%s\n", name, inputs[name])
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
