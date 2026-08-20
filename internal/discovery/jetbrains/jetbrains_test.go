package jetbrains

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRunConfig(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, ".idea", "runConfigurations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportMissingDirectoryIsNotAnError(t *testing.T) {
	got, err := Import(t.TempDir())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestImportGoApplicationConfiguration(t *testing.T) {
	root := t.TempDir()
	writeRunConfig(t, root, "api.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="api" type="GoApplicationRunConfiguration" factoryName="Go Application" folderName="Backend">
    <working_directory value="$PROJECT_DIR$/cmd/api" />
    <parameters value="--port 8080 --debug" />
    <envs>
      <env name="LOG_LEVEL" value="debug" />
    </envs>
    <method v="2" />
  </configuration>
</component>`)

	got, err := Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d configs, want 1: %+v", len(got), got)
	}
	rc := got[0]
	if !rc.IsGo {
		t.Errorf("IsGo = false, want true")
	}
	if rc.Directory != filepath.Join(root, "cmd", "api") {
		t.Errorf("Directory = %q, want %q", rc.Directory, filepath.Join(root, "cmd", "api"))
	}
	if len(rc.Args) != 3 || rc.Args[0] != "--port" || rc.Args[2] != "--debug" {
		t.Errorf("Args = %v, want [--port 8080 --debug]", rc.Args)
	}
	if rc.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("Env[LOG_LEVEL] = %q, want debug", rc.Env["LOG_LEVEL"])
	}
	if len(rc.Group) != 1 || rc.Group[0] != "Backend" {
		t.Errorf("Group = %v, want [Backend]", rc.Group)
	}
	if len(rc.Command) != 0 {
		t.Errorf("Go configs should carry no Command, got %v", rc.Command)
	}
}

func TestImportNodeConfiguration(t *testing.T) {
	root := t.TempDir()
	writeRunConfig(t, root, "worker.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="worker" type="NodeJSConfigurationType" factoryName="Node.js" folderName="Backend">
    <working-dir value="$PROJECT_DIR$/services/worker" />
    <path value="$PROJECT_DIR$/services/worker/index.js" />
    <parameters value="" />
    <envs>
      <env name="NODE_ENV" value="development" />
    </envs>
    <method v="2" />
  </configuration>
</component>`)

	got, err := Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d configs, want 1: %+v", len(got), got)
	}
	rc := got[0]
	if rc.IsGo {
		t.Errorf("IsGo = true, want false")
	}
	wantDir := filepath.Join(root, "services", "worker")
	if rc.Directory != wantDir {
		t.Errorf("Directory = %q, want %q", rc.Directory, wantDir)
	}
	wantEntry := filepath.Join(root, "services", "worker", "index.js")
	if len(rc.Command) != 2 || rc.Command[0] != "node" || rc.Command[1] != wantEntry {
		t.Errorf("Command = %v, want [node %s]", rc.Command, wantEntry)
	}
	if rc.Env["NODE_ENV"] != "development" {
		t.Errorf("Env[NODE_ENV] = %q, want development", rc.Env["NODE_ENV"])
	}
}

func TestImportNpmConfiguration(t *testing.T) {
	root := t.TempDir()
	writeRunConfig(t, root, "web-dev.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="web: dev" type="js.build_tools.npm" folderName="Frontend">
    <package-json value="$PROJECT_DIR$/web/package.json" />
    <command value="run" />
    <scripts>
      <script value="dev" />
    </scripts>
    <method v="2" />
  </configuration>
</component>`)

	got, err := Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d configs, want 1: %+v", len(got), got)
	}
	rc := got[0]
	wantDir := filepath.Join(root, "web")
	if rc.Directory != wantDir {
		t.Errorf("Directory = %q, want %q", rc.Directory, wantDir)
	}
	if len(rc.Command) != 3 || rc.Command[0] != "npm" || rc.Command[1] != "run" || rc.Command[2] != "dev" {
		t.Errorf("Command = %v, want [npm run dev]", rc.Command)
	}
	if len(rc.Group) != 1 || rc.Group[0] != "Frontend" {
		t.Errorf("Group = %v, want [Frontend]", rc.Group)
	}
}

func TestImportShellConfiguration(t *testing.T) {
	root := t.TempDir()
	writeRunConfig(t, root, "migrate.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="migrate" type="ShConfigurationType" folderName="Tools">
    <option name="SCRIPT_PATH" value="$PROJECT_DIR$/scripts/migrate.sh" />
    <option name="SCRIPT_OPTIONS" value="--up" />
    <option name="SCRIPT_WORKING_DIRECTORY" value="$PROJECT_DIR$" />
    <option name="INTERPRETER_PATH" value="/bin/bash" />
    <method v="2" />
  </configuration>
</component>`)

	got, err := Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d configs, want 1: %+v", len(got), got)
	}
	rc := got[0]
	if rc.Directory != root {
		t.Errorf("Directory = %q, want %q", rc.Directory, root)
	}
	wantScript := filepath.Join(root, "scripts", "migrate.sh")
	if len(rc.Command) != 3 || rc.Command[0] != "/bin/bash" || rc.Command[1] != wantScript || rc.Command[2] != "--up" {
		t.Errorf("Command = %v, want [/bin/bash %s --up]", rc.Command, wantScript)
	}
}

func TestImportIgnoresUnknownConfigurationTypes(t *testing.T) {
	root := t.TempDir()
	writeRunConfig(t, root, "docker.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="compose" type="docker-deploy" folderName="Infra">
    <method v="2" />
  </configuration>
</component>`)

	got, err := Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d configs, want 0 (unknown type should be ignored): %+v", len(got), got)
	}
}

func TestImportMultipleFilesAndSkipsMalformed(t *testing.T) {
	root := t.TempDir()
	writeRunConfig(t, root, "api.xml", `<component name="ProjectRunConfigurationManager">
  <configuration name="api" type="GoApplicationRunConfiguration" folderName="Backend">
    <working_directory value="$PROJECT_DIR$/cmd/api" />
  </configuration>
</component>`)
	writeRunConfig(t, root, "broken.xml", `not even xml`)

	got, err := Import(root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d configs, want 1 (malformed file should be skipped, not fatal): %+v", len(got), got)
	}
}
