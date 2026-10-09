package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gosvm/internal/targets"
)

// TargetProfile is the default profile: the only target, or the first of targets.
func (c Config) TargetProfile() (target.Profile, error) {
	ps, err := c.TargetProfiles()
	if err != nil {
		return target.Profile{}, err
	}
	return ps[0], nil
}

// TargetProfiles resolves every configured target. A "targets" list names
// networks (fogo, solana) sharing one modern backend, so it records no single
// ISA. An absent target retains exactly the historical v1.51 experimental-v3 meaning.
func (c Config) TargetProfiles() ([]target.Profile, error) {
	if len(c.Targets) != 0 {
		if c.Target != "" || c.SBF != "" || c.PlatformTools != target.ModernTools {
			return nil, fmt.Errorf("targets requires platform_tools %s and no target/sbf fields", target.ModernTools)
		}
		var ps []target.Profile
		seen := map[string]bool{}
		for _, name := range c.Targets {
			p, err := target.Resolve(name)
			if err != nil {
				return nil, err
			}
			if p.Tools != target.ModernTools || seen[p.Name] {
				return nil, fmt.Errorf("targets must list distinct modern networks (fogo, solana); got %q", name)
			}
			seen[p.Name] = true
			ps = append(ps, p)
		}
		return ps, nil
	}
	p, err := c.singleProfile()
	if err != nil {
		return nil, err
	}
	return []target.Profile{p}, nil
}

func (c Config) singleProfile() (target.Profile, error) {
	name := c.Target
	if name == "" {
		if c.PlatformTools != Tools || c.SBF != "v3" {
			return target.Profile{}, fmt.Errorf("historical projects require platform-tools v1.51 / v3; migrate explicitly with gosvm migrate-target -target <profile>")
		}
		name = target.Legacy
	}
	p, err := target.Resolve(name)
	if err != nil {
		return p, err
	}
	if c.PlatformTools != p.Tools || c.SBF != p.Arch {
		return p, fmt.Errorf("target %s requires platform_tools %s and sbf %s; migrate explicitly", p.Name, p.Tools, p.Arch)
	}
	return p, nil
}

// MigrateTarget changes configuration only, preserving source, layouts and clients.
// A comma-separated list of networks (e.g. "fogo,solana") selects several targets.
func MigrateTarget(dir, name string) error {
	names := strings.Split(name, ",")
	var c Config
	var err error
	if len(names) > 1 {
		probe := Config{Targets: names, PlatformTools: target.ModernTools}
		if _, err = probe.TargetProfiles(); err != nil {
			return err
		}
		if c, err = Load(dir); err != nil {
			return err
		}
		c.Target, c.Targets, c.PlatformTools, c.SBF = "", names, target.ModernTools, ""
	} else {
		p, err := target.Resolve(name)
		if err != nil {
			return err
		}
		if c, err = Load(dir); err != nil {
			return err
		}
		c.Target, c.Targets, c.PlatformTools, c.SBF = p.Name, nil, p.Tools, p.Arch
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gosvm-target-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(f.Name(), 0644); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "gosvm.json"))
}

func NewModuleTarget(path, module string, schema, sdkVersion int, name string) error {
	for _, n := range strings.Split(name, ",") {
		if _, err := target.Resolve(n); err != nil {
			return err
		}
	}
	if err := NewModuleSDK(path, module, schema, sdkVersion); err != nil {
		return err
	}
	return MigrateTarget(path, name)
}
