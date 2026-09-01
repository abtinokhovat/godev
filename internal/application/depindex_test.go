package application

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abtinokhovat/godev/internal/domain"
)

// writeDepFixture builds a small module on disk: two independent
// services (api, worker) plus a shared package api alone imports, so
// tests can tell "affects only api" apart from "affects everyone".
// Both main()s loop-sleep forever rather than returning immediately -
// the dep-index tests never actually start these services and don't
// care, but a test that does (see watch_test.go) needs the process to
// stay up rather than exit right after launch and register as a
// spurious crash. `select {}` would "block forever" too, but with
// only the main goroutine running that's a genuine deadlock, which
// the Go runtime kills the process for (exit status 2) - not the same
// thing as staying up.
func writeDepFixture(t *testing.T) (root string, api, worker domain.Service) {
	t.Helper()
	root = t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "cmd", "api"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "cmd", "worker"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "pkg", "shared"), 0o755))
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.24\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "pkg", "shared", "shared.go"), []byte("package shared\nfunc Do() {}\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "cmd", "api", "main.go"),
		[]byte("package main\nimport (\n\t\"fixture/pkg/shared\"\n\t\"time\"\n)\nfunc main() { shared.Do(); for { time.Sleep(time.Hour) } }\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "cmd", "worker", "main.go"),
		[]byte("package main\nimport \"time\"\nfunc main() { for { time.Sleep(time.Hour) } }\n"), 0o644))

	api = domain.Service{Name: "api", Package: "./cmd/api", Directory: filepath.Join(root, "cmd", "api"), HotReload: true}
	worker = domain.Service{Name: "worker", Package: "./cmd/worker", Directory: filepath.Join(root, "cmd", "worker"), HotReload: true}
	return root, api, worker
}

func TestComputeDepIndexIncludesOwnAndTransitiveDirs(t *testing.T) {
	root, api, worker := writeDepFixture(t)

	idx := computeDepIndex(root, []domain.Service{api, worker})

	apiDirs, ok := idx["api"]
	if !ok {
		t.Fatalf("no dep entry for api")
	}
	if _, ok := apiDirs[api.Directory]; !ok {
		t.Errorf("api's own directory missing from its dep set: %v", apiDirs)
	}
	sharedDir := filepath.Join(root, "pkg", "shared")
	if _, ok := apiDirs[sharedDir]; !ok {
		t.Errorf("api's transitive dependency (pkg/shared) missing from its dep set: %v", apiDirs)
	}

	workerDirs, ok := idx["worker"]
	if !ok {
		t.Fatalf("no dep entry for worker")
	}
	if _, ok := workerDirs[sharedDir]; ok {
		t.Errorf("worker doesn't import pkg/shared, but it showed up in its dep set: %v", workerDirs)
	}
}

func TestDepIndexNotReadyUntilSet(t *testing.T) {
	d := newDepIndex()
	if _, ok := d.servicesForDir("/anywhere"); ok {
		t.Fatal("a fresh depIndex should report not-ready")
	}
	d.set(map[string]map[string]struct{}{"api": {"/proj/cmd/api": {}}})
	names, ok := d.servicesForDir("/proj/cmd/api")
	if !ok || len(names) != 1 || names[0] != "api" {
		t.Fatalf("servicesForDir after set = %v, %v; want [api], true", names, ok)
	}
}

func TestAffectedByPathsScopesToOwningService(t *testing.T) {
	root, api, worker := writeDepFixture(t)
	sup, err := NewSupervisor(root, []domain.Service{api, worker})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	sup.rebuildDepIndex()

	// A change only inside api's own directory affects just api.
	affected, scoped := sup.affectedByPaths([]string{filepath.Join(api.Directory, "main.go")})
	if !scoped {
		t.Fatal("expected scoped=true once the dep index is populated")
	}
	if !affected["api"] || affected["worker"] {
		t.Fatalf("affected = %v, want only api", affected)
	}

	// A change to the shared package both depend on... only api
	// actually imports it, so only api should be affected.
	affected, scoped = sup.affectedByPaths([]string{filepath.Join(root, "pkg", "shared", "shared.go")})
	if !scoped {
		t.Fatal("expected scoped=true")
	}
	if !affected["api"] || affected["worker"] {
		t.Fatalf("affected = %v, want only api (worker doesn't import pkg/shared)", affected)
	}

	// go.mod/go.sum changes are never scoped - conservatively affects everyone.
	_, scoped = sup.affectedByPaths([]string{filepath.Join(root, "go.mod")})
	if scoped {
		t.Fatal("a go.mod change should never be scoped (affects everyone)")
	}
}

func TestAffectedByPathsUnscopedBeforeIndexReady(t *testing.T) {
	root, api, worker := writeDepFixture(t)
	sup, err := NewSupervisor(root, []domain.Service{api, worker})
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}
	// Deliberately not calling rebuildDepIndex - simulates a change
	// arriving before the background warm-up finishes.
	_, scoped := sup.affectedByPaths([]string{filepath.Join(api.Directory, "main.go")})
	if scoped {
		t.Fatal("expected scoped=false (affects everyone) before the index is ready")
	}
}
