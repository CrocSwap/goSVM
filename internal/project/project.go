// Package project provides the deliberately small experimental project format.
package project

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gosvm/solana"
)

const Schema = 1
const Tools = "v1.51"
const Validator = "3.0.15"

//go:embed templates/*
var templates embed.FS

type Config struct {
	Schema        int    `json:"schema"`
	Name          string `json:"name"`
	Module        string `json:"module"`
	PlatformTools string `json:"platform_tools"`
	SBF           string `json:"sbf"`
	State         string `json:"state"`
	Instruction   string `json:"instruction"`
	Handler       string `json:"handler"`
	Result        string `json:"result"`
	SDKHash       string `json:"sdk_sha256"`
}

func sdkHash() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(solana.LegacySource))) }

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var identRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

func Load(dir string) (Config, error) {
	var c Config
	b, e := os.ReadFile(filepath.Join(dir, "gosvm.json"))
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return c, fmt.Errorf("gosvm.json must contain exactly one JSON object")
	}
	if c.Schema != Schema || c.PlatformTools != Tools || c.SBF != "v3" {
		return c, fmt.Errorf("unsupported project versions; require schema %d, platform-tools %s, SBF v3", Schema, Tools)
	}
	if !nameRE.MatchString(c.Name) || !validModule(c.Module) {
		return c, fmt.Errorf("invalid project name or Go module import path")
	}
	for _, n := range []string{c.State, c.Instruction, c.Handler, c.Result} {
		if !identRE.MatchString(n) {
			return c, fmt.Errorf("invalid exported binding name %q", n)
		}
	}
	b, e = os.ReadFile(filepath.Join(dir, ".gosvm/sdk/solana/api.go"))
	if e != nil {
		return c, e
	}
	if c.SDKHash != sdkHash() || !bytes.Equal(b, []byte(solana.LegacySource)) {
		return c, fmt.Errorf("project SDK differs from this compiler; recreate with a matching compiler before building")
	}
	sdkDir := filepath.Join(dir, ".gosvm/sdk/solana")
	entries, e := os.ReadDir(sdkDir)
	if e != nil {
		return c, e
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") && entry.Name() != "api.go" {
			return c, fmt.Errorf("unexpected SDK source %s; SDK snapshot must match compiler", entry.Name())
		}
	}
	sdkMod, e := os.ReadFile(filepath.Join(dir, ".gosvm/sdk/go.mod"))
	if e != nil {
		return c, e
	}
	if string(sdkMod) != "module gosvm\n\ngo 1.22\n" {
		return c, fmt.Errorf("SDK module differs from compiler snapshot")
	}
	mod, e := os.ReadFile(filepath.Join(dir, "go.mod"))
	if e != nil {
		return c, e
	}
	if e = checkModule(string(mod), c.Module); e != nil {
		return c, e
	}

	return c, nil
}

func New(path string) error { return NewModule(path, "") }

func NewModule(path, module string) error {
	name := filepath.Base(filepath.Clean(path))
	if !nameRE.MatchString(name) {
		return fmt.Errorf("project name must match [a-z][a-z0-9-]*")
	}
	if module == "" {
		module = "example.com/" + name
	}
	if !validModule(module) {
		return fmt.Errorf("invalid Go module import path %q", module)
	}
	// Never merge a scaffold into an existing directory, including partially built projects.
	if _, e := os.Lstat(path); e == nil {
		return fmt.Errorf("%s already exists", path)
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	tmp, e := os.MkdirTemp(filepath.Dir(path), ".gosvm-new-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	c := Config{Schema: Schema, Name: name, Module: module, PlatformTools: Tools, SBF: "v3", State: "Pool", Instruction: "SwapArgs", Handler: "Swap", Result: "SwapResult", SDKHash: sdkHash()}
	entries, e := templates.ReadDir("templates")
	if e != nil {
		return e
	}
	for _, entry := range entries {
		b, e := templates.ReadFile("templates/" + entry.Name())
		if e != nil {
			return e
		}
		b = []byte(strings.ReplaceAll(string(b), "MODULE", c.Module))
		out := strings.TrimSuffix(entry.Name(), ".tmpl")
		if out == "sbf.json" {
			out = "testdata/sbf.json"
		}
		if e = writeChanged(filepath.Join(tmp, out), b); e != nil {
			return e
		}
	}
	files := map[string][]byte{"go.mod": []byte("module " + c.Module + "\n\ngo 1.22\n\nrequire gosvm v0.0.0\n\nreplace gosvm => ./.gosvm/sdk\n"), ".gosvm/sdk/go.mod": []byte("module gosvm\n\ngo 1.22\n"), ".gosvm/sdk/solana/api.go": []byte(solana.LegacySource), ".gitignore": []byte("/build/\n")}
	config, _ := json.MarshalIndent(c, "", "  ")
	files["gosvm.json"] = append(config, '\n')
	for p, b := range files {
		if e = writeChanged(filepath.Join(tmp, p), b); e != nil {
			return e
		}
	}
	if e = Generate(tmp); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

func writeChanged(path string, b []byte) error {
	if old, e := os.ReadFile(path); e == nil && bytes.Equal(old, b) {
		return nil
	}
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	return os.WriteFile(path, b, 0644)
}

// Find discovers the nearest project, without crossing a separate Go module.
func Find(start string) (string, error) {
	dir, e := filepath.Abs(start)
	if e != nil {
		return "", e
	}
	st, e := os.Stat(dir)
	if e != nil {
		return "", e
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", start)
	}
	for {
		if st, e := os.Stat(filepath.Join(dir, "gosvm.json")); e == nil && !st.IsDir() {
			return dir, nil
		} else if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if _, e := os.Stat(filepath.Join(dir, "go.mod")); e == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no gosvm.json found from %s; run gosvm new <directory>, or select a project with -dir", start)
}
func validModule(module string) bool {
	if module == "gosvm" || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._~/-]*$`).MatchString(module) {
		return false
	}
	for _, p := range strings.Split(module, "/") {
		if p == "" || p == "." || p == ".." || strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".") {
			return false
		}
	}
	return true
}

// checkModule accepts gofmt/go mod edit's single-line or block replacement forms.
// Native tests must resolve gosvm to the same SDK snapshot used by the compiler.
func checkModule(text, module string) error {
	foundModule, foundSDK := false, false
	inReplace := false
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		for i, v := range fields {
			if strings.HasPrefix(v, `"`) {
				s, e := strconv.Unquote(v)
				if e != nil {
					return fmt.Errorf("invalid quoted go.mod directive: %w", e)
				}
				fields[i] = s
			}
		}
		if fields[0] == "module" {
			if len(fields) != 2 || fields[1] != module || foundModule {
				return fmt.Errorf("go.mod module does not match gosvm.json")
			}
			foundModule = true
			continue
		}
		if fields[0] == "replace" {
			fields = fields[1:]
			if len(fields) == 1 && fields[0] == "(" {
				inReplace = true
				continue
			}
		} else if inReplace {
			if fields[0] == ")" {
				inReplace = false
				continue
			}
		} else {
			continue
		}
		if len(fields) > 0 && fields[0] == "gosvm" {
			if foundSDK || len(fields) != 3 || fields[1] != "=>" || fields[2] != "./.gosvm/sdk" {
				return fmt.Errorf("go.mod must replace gosvm with ./.gosvm/sdk without version overrides")
			}
			foundSDK = true
		}
	}
	if !foundModule {
		return fmt.Errorf("go.mod module does not match gosvm.json")
	}
	if !foundSDK || inReplace {
		return fmt.Errorf("go.mod must replace gosvm with ./.gosvm/sdk")
	}
	return nil
}
