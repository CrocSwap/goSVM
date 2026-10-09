package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gosvm/internal/targets"
)

// An absent profile retains exactly the historical v1.51 experimental-v3 meaning.
func (c Config) TargetProfile() (target.Profile, error) {
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
func MigrateTarget(dir, name string) error {
	p, err := target.Resolve(name)
	if err != nil {
		return err
	}
	c, err := Load(dir)
	if err != nil {
		return err
	}
	c.Target, c.PlatformTools, c.SBF = p.Name, p.Tools, p.Arch
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
	if _, err := target.Resolve(name); err != nil {
		return err
	}
	if err := NewModuleSDK(path, module, schema, sdkVersion); err != nil {
		return err
	}
	return MigrateTarget(path, name)
}
