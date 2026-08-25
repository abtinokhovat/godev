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
	Path        string            `yaml:"path,omitempty"`      // Go import path, e.g. "./cmd/api" - overrides a discovered service's package, or (with no discovered match) defines a brand new buildable Go service on its own. Mutually exclusive with Command.
	Command     []string          `yaml:"command,omitempty"`   // explicit run command for a standalone (non-Go) service, e.g. ["node","server.js"]
	Directory   string            `yaml:"directory,omitempty"` // working directory for a standalone service; relative paths resolve against the project root
	Args        []string          `yaml:"args,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	AutoStart   *bool             `yaml:"auto_start,omitempty"`
	AutoRestart *bool             `yaml:"auto_restart,omitempty"`
	HotReload   *bool             `yaml:"hot_reload,omitempty"`
	Watch       WatchConfig       `yaml:"watch,omitempty"`
	Group       []string          `yaml:"group,omitempty"`
}

type WatchConfig struct {
	Include []string `yaml:"include,omitempty"`
	Exclude []string `yaml:"exclude,omitempty"`
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
// a name that discovery didn't find - either a non-Go service (Command
// set) or, since godev no longer discovers Go packages at runtime, a Go
// service defined directly by its import path (Path set). The name
// itself is just the .godev.yaml map key - it never has to match the
// package/directory it points at, so renaming a service is a matter of
// renaming its key, not moving code.
func newStandaloneService(projectRoot, name string, sc ServiceConfig) (domain.Service, error) {
	switch {
	case len(sc.Command) > 0 && sc.Path != "":
		return domain.Service{}, fmt.Errorf("both \"command\" and \"path\" set - a service is either a Go package (path) or an explicit command, not both")
	case len(sc.Command) == 0 && sc.Path == "":
		return domain.Service{}, fmt.Errorf("no discovered service by this name, and neither \"command\" nor \"path\" given to define it")
	}
	isGo := sc.Path != ""

	dir := sc.Directory
	if dir == "" {
		if isGo {
			dir = filepath.Join(projectRoot, strings.TrimPrefix(sc.Path, "./"))
		} else {
			dir = projectRoot
		}
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectRoot, dir)
	}

	svc := domain.Service{
		Name:        name,
		Package:     sc.Path,
		Command:     sc.Command,
		Directory:   dir,
		Args:        sc.Args,
		Env:         sc.Env,
		AutoStart:   true,
		AutoRestart: true,
		HotReload:   isGo, // Go services rebuild-and-restart by default; command-based ones have nothing to rebuild, so it stays off unless explicitly requested
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
