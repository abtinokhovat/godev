package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/abtinokhovat/godev/internal/application"
	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/discovery"
	"github.com/abtinokhovat/godev/internal/domain"
)

// project bundles a project root with its resolved service list.
//
// Loading a project only ever reads .godev.yaml - it never runs `go
// list` or scans .idea/runConfigurations itself. Discovery is
// exclusively `godev init`'s job (see initmenu.go): it's interactive,
// it's where the user curates what actually gets added and whether it
// auto-starts, and it only ever runs once (or whenever the user asks
// for it again), not on every single `godev` invocation. That's what
// keeps `godev` itself fast regardless of how large the project is.
type project struct {
	Root     string
	Services []domain.Service
}

// resolveRoot finds the project root: the nearest go.mod walking up
// from cwd, or cwd itself when there is none - a directory with only
// .godev.yaml/JetBrains-imported (non-Go) services still works.
func resolveRoot(cwd string) (root string, isGoModule bool, err error) {
	root, ferr := discovery.FindProjectRoot(cwd)
	if ferr == nil {
		return root, true, nil
	}
	if errors.Is(ferr, discovery.ErrNoGoMod) {
		return cwd, false, nil
	}
	return "", false, ferr
}

func loadProject() (*project, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, _, err := resolveRoot(cwd)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", config.FileName, err)
	}
	if cfg == nil || len(cfg.Services) == 0 {
		return nil, fmt.Errorf("no %s found under %s - run `godev init` to discover and select services", config.FileName, root)
	}

	services, err := config.Merge(root, nil, cfg)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no %s found under %s - run `godev init` to discover and select services", config.FileName, root)
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

// resolveTargets expands `godev run <target>...` into the deduplicated
// set of services those targets name - see domain.ResolveTargets,
// which does the actual work (shared with the TUI's own ad-hoc ":"
// run prompt, so both accept exactly the same syntax).
func resolveTargets(services []domain.Service, targets []string) ([]domain.Service, error) {
	return domain.ResolveTargets(services, targets)
}

// resolveRunServices is resolveTargets with one relaxation: an empty
// target list means every service, matching bare `godev`'s behavior -
// used by the detach/daemon path, where "run everything" (`godev
// --detach`) and "run this subset" (`godev run <target>... --detach`)
// share one code path. A non-empty target list still goes through
// resolveTargets, so a typo is reported rather than silently starting
// a smaller set.
func resolveRunServices(p *project, targets []string) ([]domain.Service, string, error) {
	if len(targets) == 0 {
		return p.Services, "all services", nil
	}
	services, err := resolveTargets(p.Services, targets)
	if err != nil {
		return nil, "", err
	}
	return services, strings.Join(targets, ", "), nil
}
