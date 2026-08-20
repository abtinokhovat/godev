package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFixture(t *testing.T, root string) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "cmd", "api"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "cmd", "worker"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "internal", "shared"), 0o755))
	must(os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.24\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "cmd", "api", "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "cmd", "worker", "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "internal", "shared", "shared.go"),
		[]byte("package shared\nfunc Do() {}\n"), 0o644))
}

func TestFindProjectRoot(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root)
	sub := filepath.Join(root, "cmd", "api")

	got, err := FindProjectRoot(sub)
	if err != nil {
		t.Fatalf("FindProjectRoot: %v", err)
	}
	if got != root {
		t.Fatalf("got root %q, want %q", got, root)
	}
}

func TestFindProjectRootNoGoMod(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindProjectRoot(dir); err != ErrNoGoMod {
		t.Fatalf("got err %v, want ErrNoGoMod", err)
	}
}

func TestDiscoverFindsOnlyMainPackages(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root)

	apps, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2: %+v", len(apps), apps)
	}
	names := map[string]bool{}
	for _, a := range apps {
		names[a.Name] = true
	}
	if !names["api"] || !names["worker"] {
		t.Fatalf("expected api and worker, got %+v", names)
	}
}

func TestResolveNameUsesCmdDir(t *testing.T) {
	name := ResolveName("./cmd/payment-worker", "/proj/cmd/payment-worker")
	if name != "payment-worker" {
		t.Fatalf("got %q, want payment-worker", name)
	}
}

// TestDiscoverRecoversPartialResultsWhenAPackageFails reproduces a
// monorepo-style failure: one package fails to resolve (a broken
// import, a submodule pulling in something outside this module, ...),
// which makes `go list -json ./...` exit non-zero, but it still prints
// valid JSON for every package that did resolve. Discover must return
// those instead of discarding the whole pass - one bad package
// shouldn't take down discovery (and therefore godev) for an entire
// project.
func TestDiscoverRecoversPartialResultsWhenAPackageFails(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "broken"), 0o755))
	must(os.WriteFile(filepath.Join(root, "broken", "broken.go"),
		[]byte("package broken\nimport \"this/does/not/exist\"\nvar _ = exist.Foo\n"), 0o644))

	apps, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v (should have recovered api/worker despite the broken package)", err)
	}
	names := map[string]bool{}
	for _, a := range apps {
		names[a.Name] = true
	}
	if !names["api"] || !names["worker"] {
		t.Fatalf("expected api and worker to still be discovered, got %+v", apps)
	}
}

// TestDiscoverErrorsWhenNothingResolves covers the other end: a
// failure severe enough (e.g. a malformed go.mod, or in practice a
// toolchain resolution error) that `go list` produces no usable
// package data at all should still surface as an error, since the
// caller can't proceed on zero information.
func TestDiscoverErrorsWhenNothingResolves(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("this is not a valid go.mod\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Discover(root); err == nil {
		t.Fatal("expected an error when go list resolves nothing usable")
	}
}
