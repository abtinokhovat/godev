package main

import (
	"fmt"
	"os"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/discovery"
	"github.com/abtinokhovat/godev/internal/discovery/jetbrains"
	"github.com/abtinokhovat/godev/internal/domain"
)

// project bundles a project root with its resolved (discovery + config
// merged) service list, per sections 5-7 and 44-45.
type project struct {
	Root     string
	Services []domain.Service
}

func loadProject() (*project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := discovery.FindProjectRoot(cwd)
	if err != nil {
		return nil, fmt.Errorf("not a Go project: no go.mod found (searched upward from %s)", cwd)
	}

	apps, err := discovery.Discover(root)
	if err != nil {
		return nil, err
	}
	services := discovery.ToServices(apps)

	services, err = applyJetBrainsImport(root, services)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", config.FileName, err)
	}
	services, err = config.Merge(root, services, cfg)
	if err != nil {
		return nil, err
	}

	if len(services) == 0 {
		return nil, fmt.Errorf("no Go main packages found under %s, and no services defined in %s", root, config.FileName)
	}

	return &project{Root: root, Services: services}, nil
}

func newSupervisor(p *project) (*application.Supervisor, error) {
	return application.NewSupervisor(p.Root, p.Services)
}

func findService(p *project, name string) (domain.Service, bool) {
	for _, s := range p.Services {
		if s.Name == name {
			return s, true
		}
	}
	return domain.Service{}, false
}

// servicesInGroup returns every service whose Group has group as a
// prefix (so a subgroup like ["backend","auth"] is included when
// asking for "backend"), for `godev run <group>`.
func servicesInGroup(services []domain.Service, group string) []domain.Service {
	var out []domain.Service
	for _, s := range services {
		if len(s.Group) > 0 && s.Group[0] == group {
			out = append(out, s)
		}
	}
	return out
}

// applyJetBrainsImport enriches discovered Go services (matched by
// directory) with args/env/group from a matching JetBrains run
// configuration, and adds every other recognized configuration type as
// a new standalone (Command-based) service - the same mechanism a
// manual .godev.yaml entry uses, just sourced from .idea/runConfigurations
// instead of typed by hand. Read-only: nothing is written back to .idea/.
func applyJetBrainsImport(root string, services []domain.Service) ([]domain.Service, error) {
	configs, err := jetbrains.Import(root)
	if err != nil {
		return nil, fmt.Errorf("importing JetBrains run configurations: %w", err)
	}
	if len(configs) == 0 {
		return services, nil
	}

	byDir := make(map[string]int, len(services))
	names := make(map[string]bool, len(services))
	for i, s := range services {
		byDir[s.Directory] = i
		names[s.Name] = true
	}

	for _, rc := range configs {
		if rc.IsGo {
			i, ok := byDir[rc.Directory]
			if !ok {
				// No discovered Go service at this directory - skip
				// rather than guess at a Package import path.
				continue
			}
			if len(rc.Args) > 0 {
				services[i].Args = rc.Args
			}
			if len(rc.Env) > 0 {
				if services[i].Env == nil {
					services[i].Env = map[string]string{}
				}
				for k, v := range rc.Env {
					services[i].Env[k] = v
				}
			}
			if len(rc.Group) > 0 {
				services[i].Group = rc.Group
			}
			continue
		}

		name := rc.Name
		for names[name] {
			name += "-2"
		}
		names[name] = true

		services = append(services, domain.Service{
			Name:        name,
			Command:     rc.Command,
			Directory:   rc.Directory,
			Env:         rc.Env,
			AutoStart:   true,
			AutoRestart: true,
			Group:       rc.Group,
		})
		byDir[rc.Directory] = len(services) - 1
	}

	return services, nil
}
