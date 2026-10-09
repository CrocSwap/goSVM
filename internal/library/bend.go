package library

import (
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"fmt"
	target "gosvm/internal/targets"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Producer struct {
	Version     int               `json:"version"`
	Kind        string            `json:"kind"`
	Target      string            `json:"targetProfile"`
	ABI         int               `json:"abiVersion"`
	Root        string            `json:"root"`
	Symbol      string            `json:"symbol"`
	Fingerprint string            `json:"schemaFingerprint"`
	Input       uint64            `json:"canonicalInputBytes"`
	Output      uint64            `json:"canonicalOutputBytes"`
	Authorized  *bool             `json:"finalElfAuthorized"`
	Selection   map[string]string `json:"selection"`
	Artifacts   map[string]struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"artifacts"`
	Toolchains struct {
		Clang struct {
			Tools   string `json:"platformTools"`
			CPU     string `json:"cpu"`
			Version uint32 `json:"sbpfVersion"`
		} `json:"anzaClang"`
	} `json:"toolchains"`
}

// ImportBend consumes Bend's agreed artifact manifest. Only explicitly allowed
// producer pins are accepted; a mixed pin still needs final-image qualification.
func ImportBend(path, digest, pkg, function, allowedTools string) (Binding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Binding{}, err
	}
	if !hashRE.MatchString(digest) || fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return Binding{}, fmt.Errorf("Bend producer manifest SHA-256 mismatch")
	}
	var m Producer
	if err = json.Unmarshal(data, &m); err != nil {
		return Binding{}, err
	}
	if m.Version != 1 || m.Kind != "bend-library-v1" || m.ABI != 1 || m.Target != target.Fogo || m.Authorized == nil || *m.Authorized || m.Toolchains.Clang.CPU != "generic" || m.Toolchains.Clang.Version != 0 || m.Toolchains.Clang.Tools != "v1.53" || allowedTools != "v1.53" {
		return Binding{}, fmt.Errorf("unsupported Bend producer target/toolchain contract; only explicit fogo-v0 / v1.53 is available, finalElfAuthorized must be false")
	}
	b := Binding{Dir: filepath.Dir(path), Metadata: map[string][]byte{path: data}, Contract: Contract{
		Schema: 1, ABI: "bend-library-v1", Root: m.Root, Symbol: m.Symbol, Fingerprint: m.Fingerprint,
		InputBytes: m.Input, OutputBytes: m.Output, Package: pkg, Function: function,
		Host: Host{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, Targets: map[string]Object{},
		Producer: Artifact{Path: filepath.Base(path), SHA256: digest},
	}}
	selected := map[string]Artifact{}
	for _, kind := range []string{"hostObject", "sbfObject", "memoryObject", "header", "schema"} {
		name := m.Selection[kind]
		record, ok := m.Artifacts[name]
		if name == "" && kind == "memoryObject" {
			continue
		}
		if !ok || filepath.Base(name) != name || name == "." {
			return b, fmt.Errorf("invalid selected Bend artifact %s", kind)
		}
		artifact := Artifact{name, record.SHA256}
		data, err := b.artifact(artifact)
		if err != nil {
			return b, err
		}
		if int64(len(data)) != record.Bytes {
			return b, fmt.Errorf("Bend artifact size mismatch: %s", name)
		}
		selected[kind] = artifact
	}
	b.Interface, b.Header, b.Host.Object = selected["schema"], selected["header"], selected["hostObject"]
	object := Object{Tools: m.Toolchains.Clang.Tools, Object: selected["sbfObject"]}
	if a, ok := selected["memoryObject"]; ok {
		object.Support = []Artifact{a}
	}
	b.Targets[m.Target] = object
	if err = validateHost(filepath.Join(b.Dir, b.Host.Object.Path)); err != nil {
		return b, err
	}
	if err = b.Validate(); err != nil {
		return b, err
	}
	return b, nil
}
func validateHost(path string) error {
	if runtime.GOOS == "darwin" {
		f, err := macho.Open(path)
		if err != nil {
			return fmt.Errorf("Bend host object is not native Mach-O: %w", err)
		}
		defer f.Close()
		cpu := macho.CpuArm64
		if runtime.GOARCH == "amd64" {
			cpu = macho.CpuAmd64
		}
		if f.Cpu != cpu || f.Type != macho.TypeObj {
			return fmt.Errorf("Bend host object architecture/type mismatch")
		}
		return nil
	}
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	machine := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		machine = elf.EM_AARCH64
	}
	if f.Machine != machine || f.Type != elf.ET_REL {
		return fmt.Errorf("Bend host object architecture/type mismatch")
	}
	return nil
}
func (b *Binding) validateProducer() error {
	if b.Producer.Path == "" {
		return fmt.Errorf("Bend binding requires pinned producer manifest")
	}
	data, err := b.artifact(b.Producer)
	if err != nil {
		return err
	}
	var m Producer
	if err = json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m.Version != 1 || m.Kind != "bend-library-v1" || m.ABI != 1 || m.Authorized == nil || *m.Authorized || m.Target != target.Fogo || m.Root != b.Root || m.Symbol != b.Symbol || m.Fingerprint != b.Fingerprint || m.Input != b.InputBytes || m.Output != b.OutputBytes || m.Toolchains.Clang.Tools != "v1.53" || m.Toolchains.Clang.CPU != "generic" || m.Toolchains.Clang.Version != 0 || len(b.Targets) != 1 {
		return fmt.Errorf("binding differs from pinned Bend producer interface/target")
	}
	object, ok := b.Targets[m.Target]
	if !ok || object.Tools != m.Toolchains.Clang.Tools {
		return fmt.Errorf("binding reinterprets producer toolchain")
	}
	pairs := map[string]Artifact{"schema": b.Interface, "header": b.Header, "hostObject": b.Host.Object, "sbfObject": object.Object}
	if len(object.Support) == 1 {
		pairs["memoryObject"] = object.Support[0]
	} else if len(object.Support) > 1 {
		return fmt.Errorf("unexpected library support objects")
	}
	if (m.Selection["memoryObject"] != "") != (len(object.Support) == 1) {
		return fmt.Errorf("library support selection mismatch")
	}
	for kind, a := range pairs {
		rec, ok := m.Artifacts[a.Path]
		if !ok || m.Selection[kind] != a.Path || rec.SHA256 != a.SHA256 {
			return fmt.Errorf("binding changes producer %s selection", kind)
		}
		data, err := b.artifact(a)
		if err != nil {
			return err
		}
		if int64(len(data)) != rec.Bytes {
			return fmt.Errorf("producer artifact size mismatch")
		}
	}
	if strings.Contains(b.Package, "/") {
		return fmt.Errorf("invalid library package name")
	}
	return nil
}
