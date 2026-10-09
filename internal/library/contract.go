// Package library implements the pinned pure Bend-library ABI v1 contract.
package library

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"gosvm/internal/targets"
)

const Filename = "gosvm.library.json"

type Artifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type Object struct {
	// Producer pins this target's producer manifest; packages bound before
	// multi-target support use the contract-level Producer instead.
	Producer *Artifact  `json:"producer,omitempty"`
	Tools    string     `json:"platform_tools"`
	Object   Artifact   `json:"object"`
	Support  []Artifact `json:"support,omitempty"`
}
type Host struct {
	GOOS   string   `json:"goos"`
	GOARCH string   `json:"goarch"`
	Object Artifact `json:"object"`
}
type Contract struct {
	Producer    *Artifact         `json:"producer,omitempty"`
	Schema      int               `json:"schema"`
	ABI         string            `json:"abi"`
	Root        string            `json:"root"`
	Symbol      string            `json:"symbol"`
	Fingerprint string            `json:"schema_fingerprint"`
	InputBytes  uint64            `json:"input_bytes"`
	OutputBytes uint64            `json:"output_bytes"`
	Interface   Artifact          `json:"interface"`
	Header      Artifact          `json:"header"`
	Host        Host              `json:"host"`
	Targets     map[string]Object `json:"targets"`
	Package     string            `json:"package"`
	Function    string            `json:"function"`
	Generated   map[string]string `json:"generated,omitempty"`
}
type Binding struct {
	Contract
	Dir        string
	ImportPath string
	Metadata   map[string][]byte
}

var hashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)
var artifactPathRE = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
var identRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("require exactly one JSON object")
	}
	return nil
}
func Read(path string) (Binding, error) {
	b := Binding{Dir: filepath.Dir(path), Metadata: map[string][]byte{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return b, err
	}
	if err = decode(data, &b.Contract); err != nil {
		return b, err
	}
	b.Metadata[path] = data
	if err = b.Validate(); err != nil {
		return b, err
	}
	return b, nil
}
func (b *Binding) artifact(a Artifact) ([]byte, error) {
	// Portable packages own their artifacts; arbitrary external paths are not accepted.
	if !hashRE.MatchString(a.SHA256) || !artifactPathRE.MatchString(a.Path) || filepath.IsAbs(a.Path) || filepath.Clean(a.Path) != a.Path || a.Path == ".." || strings.HasPrefix(a.Path, "../") {
		return nil, fmt.Errorf("invalid library artifact path/hash %q", a.Path)
	}
	path := filepath.Join(b.Dir, a.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != a.SHA256 {
		return nil, fmt.Errorf("library artifact hash mismatch: %s", a.Path)
	}
	b.Metadata[path] = data
	return data, nil
}
func (b *Binding) Validate() error {
	if b.Schema != 1 || SymbolPrefix[b.ABI] == "" || !hashRE.MatchString(b.Fingerprint) || b.Root == "" || b.Symbol != SymbolPrefix[b.ABI]+b.Fingerprint || !identRE.MatchString(b.Package) || !token.IsIdentifier(b.Package) || b.Package == "main" || !identRE.MatchString(b.Function) || !token.IsExported(b.Function) || b.InputBytes == 0 || b.OutputBytes == 0 || b.InputBytes > 1024 || b.OutputBytes > 1024 || len(b.Targets) == 0 {
		return fmt.Errorf("incompatible Bend library interface; require pinned pure ABI v1, canonical bounded widths, package/function and fingerprinted symbol")
	}
	if err := b.validateProducer(); err != nil {
		return err
	}
	schema, err := b.artifact(b.Interface)
	if err != nil {
		return err
	}
	var wire struct {
		Version     int    `json:"version"`
		ABI         int    `json:"abiVersion"`
		Root        string `json:"root"`
		Symbol      string `json:"symbol"`
		Fingerprint string `json:"schemaFingerprint"`
		Input       uint64 `json:"inputBytes"`
		Output      uint64 `json:"outputBytes"`
	}
	if err = json.Unmarshal(schema, &wire); err != nil {
		return err
	}
	if wire.Version != 1 || wire.ABI != 1 || wire.Root != b.Root || wire.Symbol != b.Symbol || wire.Fingerprint != b.Fingerprint || wire.Input != b.InputBytes || wire.Output != b.OutputBytes {
		return fmt.Errorf("Bend root/schema/width mismatch")
	}
	header, err := b.artifact(b.Header)
	if err != nil {
		return err
	}
	normalized := strings.Join(strings.Fields(string(header)), "")
	signature := "uint32_t" + b.Symbol + "(constuint8_t*input,uint64_tinput_len,uint8_t*output,uint64_toutput_capacity,uint64_t*output_len);"
	checks := []string{
		"#defineBEND_LIBRARY_ABI_VERSION1u", "#defineBEND_LIBRARY_SCHEMA_FINGERPRINT\"" + b.Fingerprint + "\"",
		fmt.Sprintf("#defineBEND_LIBRARY_INPUT_BYTES%du", b.InputBytes), fmt.Sprintf("#defineBEND_LIBRARY_OUTPUT_BYTES%du", b.OutputBytes),
		"#defineBEND_LIBRARY_ABI_OK0u", "#defineBEND_LIBRARY_ABI_NULL1u", "#defineBEND_LIBRARY_ABI_RANGE2u", "#defineBEND_LIBRARY_ABI_ALIAS3u", "#defineBEND_LIBRARY_ABI_MALFORMED_INPUT4u", "#defineBEND_LIBRARY_ABI_SHORT_OUTPUT5u", "#defineBEND_LIBRARY_ABI_MISALIGNED_OUTPUT_LEN6u", "#defineBEND_LIBRARY_ABI_OUTPUT_ENCODING7u",
	}
	for _, check := range checks {
		if !strings.Contains(normalized, check) {
			return fmt.Errorf("Bend header ABI/schema/status mismatch: %s", check)
		}
	}
	if !strings.Contains(normalized, signature) {
		return fmt.Errorf("incompatible Bend C interface signature")
	}
	if b.Host.GOOS != runtime.GOOS || b.Host.GOARCH != runtime.GOARCH {
		return fmt.Errorf("Bend host object requires %s/%s, this host is %s/%s", b.Host.GOOS, b.Host.GOARCH, runtime.GOOS, runtime.GOARCH)
	}
	if err = validateHost(filepath.Join(b.Dir, b.Host.Object.Path)); err != nil {
		return err
	}
	if _, err = b.artifact(b.Host.Object); err != nil {
		return err
	}
	for name, object := range b.Targets {
		p, err := target.Resolve(name)
		if err != nil {
			return err
		}
		if p.Name != name || AcceptedTools(b.ABI, name) == "" || object.Tools != AcceptedTools(b.ABI, name) {
			return fmt.Errorf("library %s requires an explicitly allowed producer target/toolchain pin", name)
		}
		for _, a := range append([]Artifact{object.Object}, object.Support...) {
			data, err := b.artifact(a)
			if err != nil {
				return err
			}
			if err = target.ValidateELF(data, p.Flags, true); err != nil {
				return fmt.Errorf("%s: %w", a.Path, err)
			}
		}
		if err = b.checkExport(object.Object); err != nil {
			return err
		}
	}
	if len(b.Generated) > 0 {
		if len(b.Generated) != 1 || b.Generated["bindings.go"] == "" {
			return fmt.Errorf("invalid generated library package inventory")
		}
		entries, err := os.ReadDir(b.Dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				continue
			}
			if (strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && name != "bindings.go") || strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".syso") {
				return fmt.Errorf("unexpected native source in generated library package: %s", name)
			}
		}
	}
	if len(b.Generated) > 0 {
		expected, err := nativeSource(b.Contract)
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(expected)) != b.Generated["bindings.go"] {
			return fmt.Errorf("generated native binding differs from the canonical ABI adapter")
		}
	}
	for file, hash := range b.Generated {
		if _, err = b.artifact(Artifact{file, hash}); err != nil {
			return err
		}
	}
	return nil
}
func (b *Binding) checkExport(a Artifact) error {
	f, err := elf.Open(filepath.Join(b.Dir, a.Path))
	if err != nil {
		return err
	}
	defer f.Close()
	symbols, err := f.Symbols()
	if err != nil {
		return err
	}
	for _, s := range symbols {
		if s.Name == b.Symbol && s.Section != elf.SHN_UNDEF && elf.ST_BIND(s.Info) == elf.STB_GLOBAL && elf.ST_TYPE(s.Info) == elf.STT_FUNC {
			return nil
		}
	}
	return fmt.Errorf("library object does not export the expected Bend root/schema symbol %s", b.Symbol)
}
func (b Binding) Objects(profile string) ([]string, error) {
	object, ok := b.Targets[profile]
	if !ok {
		network := profile
		if p, err := target.Resolve(profile); err == nil && p.Network() != "" {
			network = p.Network()
		}
		return nil, fmt.Errorf("library %s (%s) has no %s build (no compatible %s object); add its %s producer manifest with gosvm library add, or build with -target for the networks it supports", b.Package, b.Root, network, profile, network)
	}
	paths := []string{filepath.Join(b.Dir, object.Object.Path)}
	// GoSVM owns memory helpers; Bend memory objects are validated/pinned but
	// deliberately not linked alongside those providers.
	return paths, nil
}
func (b Binding) Stub() []byte {
	return []byte(fmt.Sprintf("package %s\nconst InputBytes uint64=%d\nconst OutputBytes uint64=%d\nconst ABI_OK uint32=0\nconst ABI_NULL uint32=1\nconst ABI_RANGE uint32=2\nconst ABI_ALIAS uint32=3\nconst ABI_MALFORMED_INPUT uint32=4\nconst ABI_SHORT_OUTPUT uint32=5\nconst ABI_MISALIGNED_OUTPUT_LEN uint32=6\nconst ABI_OUTPUT_ENCODING uint32=7\nfunc %s(input, output []byte)(uint32,uint64){return 0,0}\n", b.Package, b.InputBytes, b.OutputBytes, b.Function))
}
