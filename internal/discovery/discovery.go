// Package discovery finds the Go project root and the runnable ("main")
// packages inside it, using go's own tooling rather than parsing source.
package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/abtinokhovat/godev/internal/domain"
)

// ErrNoGoMod is returned by FindProjectRoot when no go.mod is found
// walking up from the starting directory.
var ErrNoGoMod = errors.New("no go.mod found")

// FindProjectRoot walks upward from dir until it finds a directory
// containing go.mod, per section 5.1 of the plan.
func FindProjectRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", ErrNoGoMod
		}
		abs = parent
	}
}

// goListPackage mirrors the subset of `go list -json` output we need.
type goListPackage struct {
	Dir        string
	ImportPath string
	Name       string
	GoFiles    []string
}

// DiscoveredApp is a candidate executable found in the project.
type DiscoveredApp struct {
	Name      string // resolved service name
	Package   string // relative import path, e.g. "./cmd/api"
	Directory string // absolute directory
}

// Discover runs `go list -json ./...` from the project root and returns
// every "main" package as a candidate service, per sections 6-7.
func Discover(projectRoot string) ([]DiscoveredApp, error) {
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("go list failed: %w\n%s", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("go list failed: %w", err)
	}

	var apps []DiscoveredApp
	dec := json.NewDecoder(strings.NewReader(string(out)))
	seen := map[string]bool{}
	for dec.More() {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err != nil {
			return nil, fmt.Errorf("parsing go list output: %w", err)
		}
		if pkg.Name != "main" || len(pkg.GoFiles) == 0 {
			continue
		}
		rel, err := filepath.Rel(projectRoot, pkg.Dir)
		if err != nil {
			rel = pkg.Dir
		}
		importPath := "./" + filepath.ToSlash(rel)

		name := ResolveName(importPath, pkg.Dir)
		for seen[name] {
			name += "-2"
		}
		seen[name] = true

		apps = append(apps, DiscoveredApp{
			Name:      name,
			Package:   importPath,
			Directory: pkg.Dir,
		})
	}
	return apps, nil
}

// ResolveName derives a service name from a package's import path, per
// section 7's priority list (minus the config override, which callers
// apply themselves before falling back to this).
func ResolveName(importPath, dir string) string {
	base := filepath.Base(dir)
	if base != "." && base != "/" {
		return base
	}
	return filepath.Base(importPath)
}

// ToServices converts discovered apps into domain.Service values with
// sensible MVP defaults (auto-start, auto-restart, hot-reload all on).
func ToServices(apps []DiscoveredApp) []domain.Service {
	services := make([]domain.Service, 0, len(apps))
	for _, a := range apps {
		services = append(services, domain.Service{
			Name:        a.Name,
			Package:     a.Package,
			Directory:   a.Directory,
			AutoStart:   true,
			AutoRestart: true,
			HotReload:   true,
		})
	}
	return services
}
