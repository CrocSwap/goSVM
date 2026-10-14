package library

import (
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"fmt"
	target "gosvm/internal/targets"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
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
		Clang producerTools `json:"anzaClang"`
		Rust  producerTools `json:"rust"`
	} `json:"toolchains"`
}

type producerTools struct {
	Tools   string `json:"platformTools"`
	CPU     string `json:"cpu"`
	Version uint32 `json:"sbpfVersion"`
}

// tools is the SBF compiler record for the producer's kind.
func (m Producer) tools() producerTools {
	if m.Kind == RustKind {
		return m.Toolchains.Rust
	}
	return m.Toolchains.Clang
}

// SymbolPrefix is each accepted producer kind's fingerprinted ABI symbol prefix.
var SymbolPrefix = map[string]string{"bend-library-v1": "bend_library_v1_call_", RustKind: RustSymbolPrefix}

// Accepts reports whether GoSVM accepts a producer kind's platform-tools pin
// for a target.
func Accepts(kind, profile, tools string) bool {
	if kind == RustKind {
		_, ok := producerISA[profile]
		return ok && tools == RustTools
	}
	for _, t := range ProducerTools[profile] {
		if t == tools {
			return true
		}
	}
	return false
}

// ProducerTools records the Bend producer platform-tools pins GoSVM accepts
// per target. fogo-v0 v1.53 is Bend's published producer, qualified with the
// v1.57 caller; solana-v3 is produced by the caller's own v1.57 backend.
// fogo-v0 v1.57 is accepted ahead of Bend publishing it (see Untested); any
// other pairing needs a new qualification before it is listed here.
var ProducerTools = map[string][]string{target.Fogo: {"v1.53", "v1.57"}, target.Solana: {"v1.57"}}

// untested lists accepted Bend pairings with no qualification run yet.
var untested = map[string]bool{target.Fogo + "/v1.57": true}

// Untested reports whether an accepted Bend target/tools pairing still awaits
// its first qualification run.
func Untested(profile, tools string) bool { return untested[profile+"/"+tools] }

// producerISA is the producer CPU/SBPF version each target requires.
var producerISA = map[string]struct {
	CPU     string
	Version uint32
}{target.Fogo: {"generic", 0}, target.Solana: {"v3", 3}}

// ProducerPin names one producer manifest by path and pinned SHA-256.
type ProducerPin struct {
	Manifest string
	SHA256   string
}

func readProducer(path, digest string) (Producer, []byte, error) {
	var m Producer
	data, err := os.ReadFile(path)
	if err != nil {
		return m, nil, err
	}
	if !hashRE.MatchString(digest) || fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return m, nil, fmt.Errorf("library producer manifest SHA-256 mismatch: %s", path)
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return m, nil, err
	}
	return m, data, m.check(m.Target)
}

// check validates the producer's contract kind and its target/toolchain pairing.
func (m Producer) check(profile string) error {
	isa, ok := producerISA[profile]
	t := m.tools()
	if m.Version != 1 || SymbolPrefix[m.Kind] == "" || m.ABI != 1 || m.Authorized == nil || *m.Authorized || !ok || m.Target != profile || !Accepts(m.Kind, profile, t.Tools) || t.CPU != isa.CPU || t.Version != isa.Version || m.Symbol != SymbolPrefix[m.Kind]+m.Fingerprint {
		return fmt.Errorf("unsupported %s producer target/toolchain contract %q/%q; accepted: Bend fogo-v0/v1.53, fogo-v0/v1.57 or solana-v3/v1.57, Rust %s; finalElfAuthorized must be false", m.Kind, m.Target, t.Tools, RustTools)
	}
	return nil
}

// ImportBend reads one producer manifest bound with an explicit tool allowance.
func ImportBend(path, digest, pkg, function, allowedTools string) (Binding, error) {
	m, _, err := readProducer(path, digest)
	if err != nil {
		return Binding{}, err
	}
	if allowedTools != m.tools().Tools {
		return Binding{}, fmt.Errorf("binding producer tools %s requires -allow-library-tools %s", m.tools().Tools, m.tools().Tools)
	}
	stage, err := os.MkdirTemp("", "gosvm-library-")
	if err != nil {
		return Binding{}, err
	}
	defer os.RemoveAll(stage)
	if err = stageLibrary(map[string]ProducerPin{m.Target: {path, digest}}, pkg, function, stage); err != nil {
		return Binding{}, err
	}
	return Read(filepath.Join(stage, Filename))
}

// stageLibrary copies each target's pinned producer manifest and selected
// artifacts into dir/<profile>/, then writes the generated package. Objects
// are copied byte-for-byte; nothing is rebuilt or relabelled.
func stageLibrary(pins map[string]ProducerPin, pkg, function, dir string) error {
	if len(pins) == 0 {
		return fmt.Errorf("library requires at least one producer manifest")
	}
	profiles := make([]string, 0, len(pins))
	for name := range pins {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	c := Contract{Schema: 1, Package: pkg, Function: function,
		Host: Host{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}, Targets: map[string]Object{}}
	for i, name := range profiles {
		p, err := target.Resolve(name)
		if err != nil {
			return err
		}
		m, data, err := readProducer(pins[name].Manifest, pins[name].SHA256)
		if err != nil {
			return err
		}
		if m.Target != p.Name {
			return fmt.Errorf("producer manifest for %s targets %s", p.Name, m.Target)
		}
		if i == 0 {
			c.ABI, c.Root, c.Symbol, c.Fingerprint, c.InputBytes, c.OutputBytes = m.Kind, m.Root, m.Symbol, m.Fingerprint, m.Input, m.Output
		} else if m.Kind != c.ABI || m.Root != c.Root || m.Symbol != c.Symbol || m.Fingerprint != c.Fingerprint || m.Input != c.InputBytes || m.Output != c.OutputBytes {
			return fmt.Errorf("producer manifests disagree on the library interface (%s)", p.Name)
		}
		if err = os.MkdirAll(filepath.Join(dir, p.Name), 0755); err != nil {
			return err
		}
		manifest := Artifact{p.Name + "/" + filepath.Base(pins[name].Manifest), pins[name].SHA256}
		if err = os.WriteFile(filepath.Join(dir, manifest.Path), data, 0644); err != nil {
			return err
		}
		selected := map[string]Artifact{}
		for _, kind := range []string{"hostObject", "sbfObject", "memoryObject", "header", "schema"} {
			base := m.Selection[kind]
			if base == "" && kind == "memoryObject" {
				continue
			}
			record, ok := m.Artifacts[base]
			if !ok || filepath.Base(base) != base || base == "." || base == ".." {
				return fmt.Errorf("invalid selected library artifact %s", kind)
			}
			data, err := os.ReadFile(filepath.Join(filepath.Dir(pins[name].Manifest), base))
			if err != nil {
				return err
			}
			if int64(len(data)) != record.Bytes || fmt.Sprintf("%x", sha256.Sum256(data)) != record.SHA256 {
				return fmt.Errorf("library artifact size/hash mismatch: %s", base)
			}
			if err = os.WriteFile(filepath.Join(dir, p.Name, base), data, 0644); err != nil {
				return err
			}
			selected[kind] = Artifact{p.Name + "/" + base, record.SHA256}
		}
		if i == 0 {
			c.Interface, c.Header, c.Host.Object = selected["schema"], selected["header"], selected["hostObject"]
		}
		object := Object{Producer: &manifest, Tools: m.tools().Tools, Object: selected["sbfObject"]}
		if a, ok := selected["memoryObject"]; ok {
			object.Support = []Artifact{a}
		}
		c.Targets[p.Name] = object
	}
	if err := validateHost(filepath.Join(dir, c.Host.Object.Path)); err != nil {
		return err
	}
	source, err := nativeSource(c)
	if err != nil {
		return err
	}
	c.Generated = map[string]string{"bindings.go": fmt.Sprintf("%x", sha256.Sum256(source))}
	if err = os.WriteFile(filepath.Join(dir, "bindings.go"), source, 0644); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, Filename), append(data, '\n'), 0644); err != nil {
		return err
	}
	_, err = Read(filepath.Join(dir, Filename))
	return err
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
	for name, object := range b.Targets {
		producer := object.Producer
		if producer == nil {
			producer = b.Producer
		}
		if producer == nil || (object.Producer != nil) == (b.Producer != nil) {
			return fmt.Errorf("library binding requires exactly one pinned producer manifest per target")
		}
		data, err := b.artifact(*producer)
		if err != nil {
			return err
		}
		var m Producer
		if err = json.Unmarshal(data, &m); err != nil {
			return err
		}
		if err = m.check(name); err != nil {
			return err
		}
		if m.Kind != b.ABI || m.Root != b.Root || m.Symbol != b.Symbol || m.Fingerprint != b.Fingerprint || m.Input != b.InputBytes || m.Output != b.OutputBytes {
			return fmt.Errorf("binding differs from pinned producer interface/target")
		}
		if object.Tools != m.tools().Tools {
			return fmt.Errorf("binding reinterprets producer toolchain")
		}
		// Artifacts sit beside their producer manifest. The shared header/schema
		// must be byte-identical across targets; the host object comes from one.
		dir := path.Dir(producer.Path)
		pairs := map[string]Artifact{"sbfObject": object.Object}
		if len(object.Support) == 1 {
			pairs["memoryObject"] = object.Support[0]
		} else if len(object.Support) > 1 {
			return fmt.Errorf("unexpected library support objects")
		}
		if (m.Selection["memoryObject"] != "") != (len(object.Support) == 1) {
			return fmt.Errorf("library support selection mismatch")
		}
		if path.Dir(b.Host.Object.Path) == dir {
			pairs["hostObject"] = b.Host.Object
		}
		for kind, a := range map[string]Artifact{"schema": b.Interface, "header": b.Header} {
			if rec, ok := m.Artifacts[m.Selection[kind]]; !ok || rec.SHA256 != a.SHA256 {
				return fmt.Errorf("binding changes producer %s selection", kind)
			}
			if path.Dir(a.Path) == dir {
				pairs[kind] = a
			}
		}
		for kind, a := range pairs {
			rec, ok := m.Artifacts[path.Base(a.Path)]
			if !ok || path.Dir(a.Path) != dir || m.Selection[kind] != path.Base(a.Path) || rec.SHA256 != a.SHA256 {
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
	}
	if strings.Contains(b.Package, "/") {
		return fmt.Errorf("invalid library package name")
	}
	return nil
}

// InspectBend checks a producer manifest's contract and returns its target,
// producer platform-tools pin and SHA-256.
func InspectBend(path string) (string, string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	m, _, err := readProducer(path, digest)
	return m.Target, m.tools().Tools, digest, err
}
