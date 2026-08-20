package main

import (
	"fmt"
	"os"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/discovery"
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
	if len(apps) == 0 {
		return nil, fmt.Errorf("no main packages found under %s", root)
	}
	services := discovery.ToServices(apps)

	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", config.FileName, err)
	}
	services = config.Merge(services, cfg)

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
