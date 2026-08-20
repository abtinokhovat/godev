// Package config loads the optional .godev.yaml file and merges it over
// discovery defaults. Configuration is always optional for Go services -
// discovery alone must produce a working service list - but it is also
// the primary way to add non-Go ("other") services, since godev never
// heuristically auto-discovers anything beyond Go main packages.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abtinokhovat/godev/internal/domain"
	"gopkg.in/yaml.v3"
)

const FileName = ".godev.yaml"

// File is the on-disk shape of .godev.yaml.
type File struct {
	Services map[string]ServiceConfig `yaml:"services"`
}

// ServiceConfig either overrides fields on a service discovery already
// found (matched by map key against the service name), or, when no
// such service exists, defines a brand new standalone service - which
// must set Command (and usually Directory), since there is no
// discoverer to supply them.
type ServiceConfig struct {
	Path        string            `yaml:"path"`      // Go: import path override, e.g. "./cmd/api". Only meaningful for discovered Go services.
	Command     []string          `yaml:"command"`   // explicit run command for a standalone (non-Go) service, e.g. ["node","server.js"]
	Directory   string            `yaml:"directory"` // working directory for a standalone service; relative paths resolve against the project root
	Args        []string          `yaml:"args"`
	Env         map[string]string `yaml:"env"`
	AutoStart   *bool             `yaml:"auto_start"`
	AutoRestart *bool             `yaml:"auto_restart"`
	HotReload   *bool             `yaml:"hot_reload"`
	Watch       WatchConfig       `yaml:"watch"`
	Group       []string          `yaml:"group"`
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

// Merge applies configuration overrides on top of discovered services,
// then appends any config entries that don't match a discovered
// service as new, standalone services. Discovered-service overrides
// are applied in discovery order (stable); standalone services are
// appended in the config file's map iteration order (Go map order is
// randomized, but service identity/behavior doesn't depend on it).
func Merge(projectRoot string, discovered []domain.Service, cfg *File) ([]domain.Service, error) {
	if cfg == nil {
		return discovered, nil
	}
	result := make([]domain.Service, len(discovered))
	copy(result, discovered)

	matched := make(map[string]bool, len(cfg.Services))
	for i := range result {
		sc, ok := cfg.Services[result[i].Name]
		if !ok {
			continue
		}
		matched[result[i].Name] = true
		applyOverride(projectRoot, &result[i], sc)
	}

	for name, sc := range cfg.Services {
		if matched[name] {
			continue
		}
		svc, err := newStandaloneService(projectRoot, name, sc)
		if err != nil {
			return nil, fmt.Errorf("service %q in %s: %w", name, FileName, err)
		}
		result = append(result, svc)
	}

	return result, nil
}

// newStandaloneService builds a domain.Service purely from config, for
// a name that discovery didn't find - the primary mechanism for
// non-Go ("other") services.
func newStandaloneService(projectRoot, name string, sc ServiceConfig) (domain.Service, error) {
	if len(sc.Command) == 0 {
		return domain.Service{}, fmt.Errorf("no discovered service by this name, and no \"command\" given to define it as a standalone service")
	}
	dir := sc.Directory
	if dir == "" {
		dir = projectRoot
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectRoot, dir)
	}

	svc := domain.Service{
		Name:        name,
		Command:     sc.Command,
		Directory:   dir,
		Args:        sc.Args,
		Env:         sc.Env,
		AutoStart:   true,
		AutoRestart: true,
		Group:       sc.Group,
		Watch:       domain.WatchConfig(sc.Watch),
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
	return svc, nil
}

// applyOverride applies a ServiceConfig on top of an already-discovered
// service. sc.Path, when set, repoints which Go package this service
// name builds - e.g. after a directory rename - and Directory is
// recomputed from it the same way discovery derives Directory from
// Package, so the two never drift out of sync.
func applyOverride(projectRoot string, svc *domain.Service, sc ServiceConfig) {
	if sc.Path != "" {
		svc.Package = sc.Path
		svc.Directory = filepath.Join(projectRoot, strings.TrimPrefix(sc.Path, "./"))
	}
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
	if len(sc.Group) > 0 {
		svc.Group = sc.Group
	}
}
