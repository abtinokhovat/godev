package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abtinokhovat/godev/internal/domain"
)

func writeMainPkg(t *testing.T, root, body string) domain.Service {
	t.Helper()
	dir := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return domain.Service{Name: "app", Package: "./cmd/app", Directory: dir}
}

func TestBuildSuccessProducesRunnableBinary(t *testing.T) {
	root := t.TempDir()
	svc := writeMainPkg(t, root, "package main\nfunc main() { println(\"hi\") }\n")

	b, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := b.Build(svc, ModeNormal)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !res.Success {
		t.Fatalf("build failed: %s", res.Output)
	}
	if _, err := os.Stat(res.BinaryPath); err != nil {
		t.Fatalf("binary missing: %v", err)
	}
}

func TestBuildFailurePreservesPreviousBinary(t *testing.T) {
	root := t.TempDir()
	svc := writeMainPkg(t, root, "package main\nfunc main() {}\n")

	b, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res1, err := b.Build(svc, ModeNormal)
	if err != nil || !res1.Success {
		t.Fatalf("first build should succeed: err=%v res=%+v", err, res1)
	}
	firstInfo, err := os.Stat(res1.BinaryPath)
	if err != nil {
		t.Fatal(err)
	}

	// Break the source.
	badPath := filepath.Join(svc.Directory, "main.go")
	if err := os.WriteFile(badPath, []byte("package main\nfunc main() { this is not go }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res2, err := b.Build(svc, ModeNormal)
	if err != nil {
		t.Fatalf("Build should return nil error on compile failure, got %v", err)
	}
	if res2.Success {
		t.Fatalf("expected build failure")
	}

	afterInfo, err := os.Stat(res1.BinaryPath)
	if err != nil {
		t.Fatalf("previous binary should still exist: %v", err)
	}
	if afterInfo.ModTime() != firstInfo.ModTime() {
		t.Fatalf("previous binary should be untouched by the failed build")
	}
}
