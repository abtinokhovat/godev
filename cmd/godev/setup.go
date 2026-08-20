package main

import (
	"fmt"
	"os"
	"strings"

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

	// No go.mod is not fatal by itself - godev also runs manually
	// configured (.godev.yaml) and JetBrains-imported non-Go services,
	// so a project with only those still works; it just has nothing
	// for Go discovery to find. Fall back to cwd as the project root
	// in that case.
	root, err := discovery.FindProjectRoot(cwd)
	isGoModule := err == nil
	if !isGoModule {
		root = cwd
	}

	// A `go list` failure (a broken package elsewhere in a monorepo, a
	// toolchain/GOSUMDB resolution error, ...) shouldn't take down
	// discovery for the whole project - degrade to whatever Go
	// services were found (possibly none) and let JetBrains import and
	// .godev.yaml still contribute, per the final "nothing runnable"
	// check below.
	var apps []discovery.DiscoveredApp
	if isGoModule {
		apps, err = discovery.Discover(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: Go package discovery failed, continuing without auto-discovered Go services: %v\n", err)
			apps = nil
		}
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
		if isGoModule {
			return nil, fmt.Errorf("no Go main packages found under %s, and no services defined in %s", root, config.FileName)
		}
		return nil, fmt.Errorf("no go.mod found under %s, and no services defined in %s", root, config.FileName)
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
// set of services those targets name, preserving first-appearance
// order. Each target is matched as a group name first (Group[0], so a
// subgroup like ["backend","auth"] matches a request for "backend"),
// then as an exact service name - group match takes precedence so a
// bare word like "core" expands to every service in that group even
// if a single service happens to share the name. A service reachable
// through more than one requested target (its own name plus a group
// it belongs to, or two overlapping groups) is only included once.
// Every target must resolve to at least one service, or the whole
// call fails listing everything that didn't match, so a typo doesn't
// silently start a smaller set than the user asked for.
func resolveTargets(services []domain.Service, targets []string) ([]domain.Service, error) {
	byName := make(map[string]domain.Service, len(services))
	for _, s := range services {
		byName[s.Name] = s
	}

	seen := make(map[string]bool, len(services))
	var out []domain.Service
	var unmatched []string

	for _, target := range targets {
		matched := false
		for _, s := range services {
			if len(s.Group) > 0 && s.Group[0] == target {
				matched = true
				if !seen[s.Name] {
					seen[s.Name] = true
					out = append(out, s)
				}
			}
		}
		if !matched {
			if s, ok := byName[target]; ok {
				matched = true
				if !seen[s.Name] {
					seen[s.Name] = true
					out = append(out, s)
				}
			}
		}
		if !matched {
			unmatched = append(unmatched, target)
		}
	}

	if len(unmatched) > 0 {
		return nil, fmt.Errorf("no group or service named %s", strings.Join(unmatched, ", "))
	}
	return out, nil
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
