package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abtinokhovat/godev/internal/domain"
)

// writeGoModule sets up a minimal go.mod + cmd/api/main.go fixture so
// gatherCandidates' own discovery.Discover call has something real to
// find, mirroring internal/discovery's own test fixtures.
func writeGoModule(t *testing.T, root string) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "cmd", "api"), 0o755))
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.24\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "cmd", "api", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644))
}

func TestGatherCandidatesSkipsAlreadyConfiguredNames(t *testing.T) {
	root := t.TempDir()
	writeGoModule(t, root)

	got, err := gatherCandidates(root, true, map[string]bool{"api": true})
	if err != nil {
		t.Fatalf("gatherCandidates: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d candidates, want 0 (already-configured name should be skipped): %+v", len(got), got)
	}
}

func TestResolveTargetsFiltersGroupByPrefix(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Group: []string{"core"}},
		{Name: "worker", Group: []string{"core"}},
		{Name: "web", Group: []string{"frontend"}},
		{Name: "scheduler"},
	}
	got, err := resolveTargets(services, []string{"core"})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Name != "api" && s.Name != "worker" {
			t.Errorf("unexpected service %q in group \"core\"", s.Name)
		}
	}
}

func TestResolveTargetsAcceptsIndividualServiceName(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Group: []string{"core"}},
		{Name: "scheduler"},
	}
	got, err := resolveTargets(services, []string{"scheduler"})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	if len(got) != 1 || got[0].Name != "scheduler" {
		t.Fatalf("got %+v, want just scheduler", got)
	}
}

func TestResolveTargetsDedupesServiceSharedAcrossGroups(t *testing.T) {
	// "shared" is in "g1"; requesting "g1" and "shared" together (as a
	// stand-in for the realistic case of two overlapping groups both
	// resolving to it) should still start it exactly once.
	services := []domain.Service{
		{Name: "shared", Group: []string{"g1"}},
		{Name: "only-g1", Group: []string{"g1"}},
		{Name: "only-g2", Group: []string{"g2"}},
	}
	got, err := resolveTargets(services, []string{"g1", "shared"})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	count := 0
	for _, s := range got {
		if s.Name == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("shared appeared %d times, want 1: %+v", count, got)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2 (shared, only-g1): %+v", len(got), got)
	}
}

func TestResolveTargetsGroupTakesPrecedenceOverSameNamedService(t *testing.T) {
	services := []domain.Service{
		{Name: "core", Group: []string{"other"}}, // a service literally named "core"
		{Name: "api", Group: []string{"core"}},
		{Name: "worker", Group: []string{"core"}},
	}
	got, err := resolveTargets(services, []string{"core"})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2 (the core group's members, not the service named \"core\"): %+v", len(got), got)
	}
	for _, s := range got {
		if s.Name == "core" {
			t.Errorf("expected group match to take precedence over the same-named service")
		}
	}
}

func TestResolveTargetsMatchesAnyOfAServicesGroups(t *testing.T) {
	// A service can list more than one group (group: [core, test] in
	// .godev.yaml) - it must be reachable by any of them, not just the
	// first, per the reported bug: `godev run test` on a service in
	// [core, test] errored "no group or service named test".
	services := []domain.Service{
		{Name: "api", Group: []string{"core", "test"}},
		{Name: "worker", Group: []string{"core"}},
		{Name: "e2e", Group: []string{"test"}},
	}

	got, err := resolveTargets(services, []string{"test"})
	if err != nil {
		t.Fatalf("resolveTargets: %v", err)
	}
	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	if !names["api"] || !names["e2e"] || names["worker"] {
		t.Fatalf("resolveTargets(test) = %v, want [api, e2e] (worker isn't in \"test\")", names)
	}
}

func TestResolveTargetsUnknownTargetErrors(t *testing.T) {
	services := []domain.Service{{Name: "api"}}
	_, err := resolveTargets(services, []string{"api", "nonexistent"})
	if err == nil {
		t.Fatal("expected an error for an unmatched target")
	}
}
