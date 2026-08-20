// Package config loads the optional .godev.yaml file and merges it over
// discovery defaults, per sections 44-45 of the plan. Configuration is
// always optional - discovery alone must produce a working service list.
package config

import (
	"os"
	"path/filepath"

	"github.com/abtinokhovat/godev/internal/domain"
	"gopkg.in/yaml.v3"
)

const FileName = ".godev.yaml"

// File is the on-disk shape of .godev.yaml.
type File struct {
	Services map[string]ServiceConfig `yaml:"services"`
}

type ServiceConfig struct {
	Path        string            `yaml:"path"`
	Args        []string          `yaml:"args"`
	Env         map[string]string `yaml:"env"`
	AutoStart   *bool             `yaml:"auto_start"`
	AutoRestart *bool             `yaml:"auto_restart"`
	HotReload   *bool             `yaml:"hot_reload"`
	Watch       WatchConfig       `yaml:"watch"`
}

type WatchConfig struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// Load reads .godev.yaml from projectRoot. A missing file is not an
// error - it returns a nil File, meaning "no overrides".
func Load(projectRoot string) (*File, error) {
	path := filepath.Join(projectRoot, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Merge applies configuration overrides on top of discovered services.
// Discovery provides the base list (and thus the set of services that
// exist); config can only override fields on services discovery found,
// keyed by service name, per section 45's merge order.
func Merge(discovered []domain.Service, cfg *File) []domain.Service {
	if cfg == nil {
		return discovered
	}
	result := make([]domain.Service, len(discovered))
	copy(result, discovered)

	for i := range result {
		sc, ok := cfg.Services[result[i].Name]
		if !ok {
			continue
		}
		applyOverride(&result[i], sc)
	}
	return result
}

func applyOverride(svc *domain.Service, sc ServiceConfig) {
	if len(sc.Args) > 0 {
		svc.Args = sc.Args
	}
	if len(sc.Env) > 0 {
		if svc.Env == nil {
			svc.Env = map[string]string{}
		}
		for k, v := range sc.Env {
			svc.Env[k] = v
		}
	}
	if sc.AutoStart != nil {
		svc.AutoStart = *sc.AutoStart
	}
	if sc.AutoRestart != nil {
		svc.AutoRestart = *sc.AutoRestart
	}
	if sc.HotReload != nil {
		svc.HotReload = *sc.HotReload
	}
	if len(sc.Watch.Include) > 0 {
		svc.Watch.Include = sc.Watch.Include
	}
	if len(sc.Watch.Exclude) > 0 {
		svc.Watch.Exclude = sc.Watch.Exclude
	}
}
