package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abtinokhovat/godev/internal/domain"
)

func writeJetBrainsConfig(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, ".idea", "runConfigurations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyJetBrainsImportEnrichesMatchingGoService(t *testing.T) {
	root := t.TempDir()
	writeJetBrainsConfig(t, root, "api.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="api" type="GoApplicationRunConfiguration" folderName="Backend">
    <working_directory value="$PROJECT_DIR$/cmd/api" />
    <parameters value="--port 9090" />
  </configuration>
</component>`)

	services := []domain.Service{
		{Name: "api", Package: "./cmd/api", Directory: filepath.Join(root, "cmd", "api")},
	}
	got, err := applyJetBrainsImport(root, services)
	if err != nil {
		t.Fatalf("applyJetBrainsImport: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1 (enrichment shouldn't add a new service): %+v", len(got), got)
	}
	if len(got[0].Args) != 2 || got[0].Args[1] != "9090" {
		t.Errorf("Args = %v, want [--port 9090]", got[0].Args)
	}
	if len(got[0].Group) != 1 || got[0].Group[0] != "Backend" {
		t.Errorf("Group = %v, want [Backend]", got[0].Group)
	}
	if got[0].IsCommand() {
		t.Errorf("enriched Go service should remain non-command, still built via go build")
	}
}

func TestApplyJetBrainsImportAddsStandaloneNonGoService(t *testing.T) {
	root := t.TempDir()
	writeJetBrainsConfig(t, root, "worker.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="worker" type="NodeJSConfigurationType" folderName="Backend">
    <working-dir value="$PROJECT_DIR$/services/worker" />
    <path value="$PROJECT_DIR$/services/worker/index.js" />
  </configuration>
</component>`)

	got, err := applyJetBrainsImport(root, nil)
	if err != nil {
		t.Fatalf("applyJetBrainsImport: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1: %+v", len(got), got)
	}
	if !got[0].IsCommand() {
		t.Errorf("imported Node config should be a command-based service")
	}
	if got[0].Name != "worker" {
		t.Errorf("Name = %q, want worker", got[0].Name)
	}
}

func TestApplyJetBrainsImportDedupesNameCollision(t *testing.T) {
	root := t.TempDir()
	writeJetBrainsConfig(t, root, "worker.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="api" type="NodeJSConfigurationType">
    <working-dir value="$PROJECT_DIR$/other" />
    <path value="$PROJECT_DIR$/other/index.js" />
  </configuration>
</component>`)

	services := []domain.Service{{Name: "api", Package: "./cmd/api", Directory: filepath.Join(root, "cmd", "api")}}
	got, err := applyJetBrainsImport(root, services)
	if err != nil {
		t.Fatalf("applyJetBrainsImport: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(got), got)
	}
	if got[1].Name == "api" {
		t.Errorf("colliding name should have been suffixed, got %q", got[1].Name)
	}
}

func TestServicesInGroupFiltersByPrefix(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Group: []string{"core"}},
		{Name: "worker", Group: []string{"core"}},
		{Name: "web", Group: []string{"frontend"}},
		{Name: "scheduler"},
	}
	got := servicesInGroup(services, "core")
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Name != "api" && s.Name != "worker" {
			t.Errorf("unexpected service %q in group \"core\"", s.Name)
		}
	}
}
