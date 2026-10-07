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

	"gosvm/sdk"
	"gosvm/solana"
)

const Schema = 1
const MultiSchema = 2
const Tools = "v1.51"
const Validator = "3.0.15"

//go:embed templates/* schema2/*
var templates embed.FS

type Config struct {
	Schema        int                      `json:"schema"`
	Name          string                   `json:"name"`
	Module        string                   `json:"module"`
	PlatformTools string                   `json:"platform_tools"`
	SBF           string                   `json:"sbf"`
	State         string                   `json:"state"`
	Instruction   string                   `json:"instruction"`
	Handler       string                   `json:"handler"`
	Result        string                   `json:"result"`
	SDKHash       string                   `json:"sdk_sha256"`
	SDKVersion    int                      `json:"sdk_version,omitempty"`
	Imports       map[string]string        `json:"imports,omitempty"`
	Layouts       []WireDeclaration        `json:"layouts,omitempty"`
	Instructions  []InstructionDeclaration `json:"instructions,omitempty"`
}

func sdkHash() string { return fmt.Sprintf("%x", sha256.Sum256([]byte(solana.LegacySource))) }

func sdkFiles(version int) map[string][]byte {
	source := solana.LegacySource
	if version == 2 {
		source = solana.Source
	}
	files := map[string][]byte{"solana/api.go": []byte(source)}
	if version == 2 {
		for _, path := range []string{"pda/pda.go", "cpi/cpi.go", "token/token.go", "token/lifecycle.go", "system/system.go", "system/rent.go"} {
			b, err := sdk.Sources.ReadFile(path)
			if err != nil {
				panic(err)
			}
			files["sdk/"+path] = b
		}
	}
	return files
}

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
	if (c.Schema != Schema && c.Schema != MultiSchema) || c.PlatformTools != Tools || c.SBF != "v3" {
		return c, fmt.Errorf("unsupported project versions; require schema 1 or 2, platform-tools %s, SBF v3", Tools)
	}
	if !nameRE.MatchString(c.Name) || !validModule(c.Module) {
		return c, fmt.Errorf("invalid project name or Go module import path")
	}
	if c.SDKVersion != 0 && c.SDKVersion != 1 && c.SDKVersion != 2 || c.Schema == Schema && c.SDKVersion == 2 {
		return c, fmt.Errorf("SDK version 2 requires schema 2; supported SDK versions are 1 and 2")
	}
	if c.Schema == Schema {
		if len(c.Imports) != 0 || len(c.Layouts) != 0 || len(c.Instructions) != 0 {
			return c, fmt.Errorf("schema 1 does not accept schema-2 declarations")
		}
		for _, n := range []string{c.State, c.Instruction, c.Handler, c.Result} {
			if !identRE.MatchString(n) {
				return c, fmt.Errorf("invalid exported binding name %q", n)
			}
		}
	} else if e := validateMultiConfig(c); e != nil {
		return c, e
	}
	files := sdkFiles(c.SDKVersion)
	if c.SDKHash != fmt.Sprintf("%x", sha256.Sum256(files["solana/api.go"])) {
		return c, fmt.Errorf("project SDK differs from this compiler; recreate with a matching compiler before building")
	}
	for name, expected := range files {
		b, e := os.ReadFile(filepath.Join(dir, ".gosvm/sdk", name))
		if e != nil {
			return c, e
		}
		if !bytes.Equal(b, expected) {
			return c, fmt.Errorf("project SDK differs from this compiler: %s", name)
		}
	}
	if e := filepath.WalkDir(filepath.Join(dir, ".gosvm/sdk"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			rel, err := filepath.Rel(filepath.Join(dir, ".gosvm/sdk"), path)
			if err != nil {
				return err
			}
			if _, ok := files[filepath.ToSlash(rel)]; !ok {
				return fmt.Errorf("unexpected SDK source %s; SDK snapshot must match compiler", rel)
			}
		}
		return nil
	}); e != nil {
		return c, e
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
	return NewModuleSchema(path, module, Schema)
}

// NewModuleSchema explicitly selects a scaffold; the default remains schema 1.
func NewModuleSchema(path, module string, schema int) error {
	return NewModuleSDK(path, module, schema, 1)
}

func NewModuleSDK(path, module string, schema, sdkVersion int) error {
	if schema != Schema && schema != MultiSchema {
		return fmt.Errorf("unsupported scaffold schema %d", schema)
	}
	if sdkVersion != 1 && sdkVersion != 2 || schema == Schema && sdkVersion != 1 {
		return fmt.Errorf("SDK version 2 requires schema 2; supported SDK versions are 1 and 2")
	}
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
	if sdkVersion == 2 {
		c.SDKVersion = 2
		c.SDKHash = fmt.Sprintf("%x", sha256.Sum256([]byte(solana.Source)))
	}
	templateDir := "templates"
	if schema == MultiSchema {
		templateDir = "schema2"
		c.Schema, c.State, c.Instruction, c.Handler, c.Result = MultiSchema, "", "", "", ""
		c.Imports = map[string]string{"model": module + "/model"}
		c.Layouts = []WireDeclaration{{Name: "Vault", Type: "model.Vault", Version: 1}, {Name: "Policy", Type: "model.Policy", Version: 1}}
		c.Instructions = []InstructionDeclaration{
			{Name: "Move", Handler: "Move", Args: "model.MoveArgs", Bundle: "MoveAccounts", Version: 1, Accounts: []AccountDeclaration{
				{Name: "Source", Kind: "state", Layout: "Vault", Access: "write", Owner: "program_id"},
				{Name: "Destination", Kind: "state", Layout: "Vault", Access: "write", Owner: "program_id"},
				{Name: "Policy", Kind: "state", Layout: "Policy", Access: "read", Owner: "program_id"},
				{Name: "Authority", Kind: "signer", Access: "read"},
			}, Relations: []KeyRelation{{Field: "Source.Authority", Account: "Authority"}, {Field: "Destination.Authority", Account: "Authority"}}},
			{Name: "SetLimit", Handler: "SetLimit", Args: "model.SetLimitArgs", Bundle: "SetLimitAccounts", Version: 1, Accounts: []AccountDeclaration{
				{Name: "Policy", Kind: "state", Layout: "Policy", Access: "write", Owner: "program_id"},
				{Name: "Authority", Kind: "signer", Access: "read"},
			}, Relations: []KeyRelation{{Field: "Policy.Authority", Account: "Authority"}}},
		}
	}
	entries, e := templates.ReadDir(templateDir)
	if e != nil {
		return e
	}
	for _, entry := range entries {
		b, e := templates.ReadFile(templateDir + "/" + entry.Name())
		if e != nil {
			return e
		}
		b = []byte(strings.ReplaceAll(string(b), "MODULE", c.Module))
		out := strings.TrimSuffix(entry.Name(), ".tmpl")
		if out == "sbf.json" {
			out = "testdata/sbf.json"
		}
		if templateDir == "schema2" && out == "model.go" {
			out = "model/model.go"
		}
		if e = writeChanged(filepath.Join(tmp, out), b); e != nil {
			return e
		}
	}
	files := map[string][]byte{"go.mod": []byte("module " + c.Module + "\n\ngo 1.22\n\nrequire gosvm v0.0.0\n\nreplace gosvm => ./.gosvm/sdk\n"), ".gosvm/sdk/go.mod": []byte("module gosvm\n\ngo 1.22\n"), ".gitignore": []byte("/build/\n")}
	for name, b := range sdkFiles(sdkVersion) {
		files[".gosvm/sdk/"+name] = b
	}
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
