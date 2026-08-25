package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abtinokhovat/godev/internal/domain"
)

func TestMergeNilConfigIsNoop(t *testing.T) {
	services := []domain.Service{{Name: "api", AutoStart: true}}
	got, err := Merge("/proj", services, nil)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(got) != 1 || got[0].Name != "api" {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestMergeOverridesOnlyMatchingService(t *testing.T) {
	services := []domain.Service{
		{Name: "api", AutoStart: true, HotReload: true},
		{Name: "worker", AutoStart: true, HotReload: true},
	}
	falseVal := false
	cfg := &File{Services: map[string]ServiceConfig{
		"api": {
			Args:      []string{"--port", "8080"},
			Env:       map[string]string{"LOG_LEVEL": "debug"},
			AutoStart: &falseVal,
			Group:     []string{"backend"},
		},
	}}

	got, err := Merge("/proj", services, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var api, worker domain.Service
	for _, s := range got {
		switch s.Name {
		case "api":
			api = s
		case "worker":
			worker = s
		}
	}

	if api.AutoStart != false {
		t.Errorf("api.AutoStart = %v, want false", api.AutoStart)
	}
	if len(api.Args) != 2 || api.Args[0] != "--port" || api.Args[1] != "8080" {
		t.Errorf("api.Args = %v, want [--port 8080]", api.Args)
	}
	if api.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("api.Env[LOG_LEVEL] = %q, want debug", api.Env["LOG_LEVEL"])
	}
	if !api.HotReload {
		t.Errorf("api.HotReload should be unchanged (true)")
	}
	if len(api.Group) != 1 || api.Group[0] != "backend" {
		t.Errorf("api.Group = %v, want [backend]", api.Group)
	}

	if !worker.AutoStart {
		t.Errorf("worker should be untouched by api's overrides")
	}
}

func TestMergeOverridesPathRepointsPackageAndDirectory(t *testing.T) {
	services := []domain.Service{
		{Name: "api", Package: "./cmd/api", Directory: "/proj/cmd/api"},
	}
	cfg := &File{Services: map[string]ServiceConfig{
		"api": {Path: "./cmd/api-v2"},
	}}

	got, err := Merge("/proj", services, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got[0].Package != "./cmd/api-v2" {
		t.Errorf("Package = %q, want ./cmd/api-v2", got[0].Package)
	}
	if got[0].Directory != "/proj/cmd/api-v2" {
		t.Errorf("Directory = %q, want /proj/cmd/api-v2 (recomputed to match the new Package)", got[0].Directory)
	}
}

func TestMergeUnmatchedEntryWithoutCommandErrors(t *testing.T) {
	services := []domain.Service{{Name: "api", Args: []string{"orig"}}}
	cfg := &File{Services: map[string]ServiceConfig{
		// No discovered "nonexistent" service, and no Command to define
		// it standalone - this must be reported, not silently dropped or
		// silently applied as a no-op override.
		"nonexistent": {Args: []string{"new"}},
	}}
	_, err := Merge("/proj", services, cfg)
	if err == nil {
		t.Fatal("expected an error for an unmatched entry with no command")
	}
}

func TestMergeAddsStandaloneCommandService(t *testing.T) {
	services := []domain.Service{{Name: "api"}}
	cfg := &File{Services: map[string]ServiceConfig{
		"web": {
			Command:   []string{"npm", "run", "dev"},
			Directory: "frontend",
			Env:       map[string]string{"PORT": "3000"},
			Group:     []string{"frontend"},
		},
	}}

	got, err := Merge("/proj", services, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d services, want 2 (api + web): %+v", len(got), got)
	}

	var web domain.Service
	found := false
	for _, s := range got {
		if s.Name == "web" {
			web, found = s, true
		}
	}
	if !found {
		t.Fatalf("standalone service %q not added", "web")
	}
	if !web.IsCommand() {
		t.Errorf("web.IsCommand() = false, want true")
	}
	if len(web.Command) != 3 || web.Command[2] != "dev" {
		t.Errorf("web.Command = %v, want [npm run dev]", web.Command)
	}
	if web.Directory != "/proj/frontend" {
		t.Errorf("web.Directory = %q, want /proj/frontend (resolved against project root)", web.Directory)
	}
	if !web.AutoStart || !web.AutoRestart {
		t.Errorf("standalone services should default AutoStart/AutoRestart to true, got %+v", web)
	}
	if len(web.Group) != 1 || web.Group[0] != "frontend" {
		t.Errorf("web.Group = %v, want [frontend]", web.Group)
	}
}

func TestMergeStandaloneServiceAbsoluteDirectoryUnchanged(t *testing.T) {
	cfg := &File{Services: map[string]ServiceConfig{
		"web": {Command: []string{"node", "server.js"}, Directory: "/abs/path"},
	}}
	got, err := Merge("/proj", nil, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got[0].Directory != "/abs/path" {
		t.Errorf("Directory = %q, want unchanged absolute path", got[0].Directory)
	}
}

func TestMergeAddsStandaloneGoServiceFromPath(t *testing.T) {
	cfg := &File{Services: map[string]ServiceConfig{
		"api": {Path: "./cmd/api"},
	}}
	got, err := Merge("/proj", nil, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1: %+v", len(got), got)
	}
	svc := got[0]
	if svc.IsCommand() {
		t.Errorf("a path-defined service should build, not exec a command")
	}
	if svc.Package != "./cmd/api" {
		t.Errorf("Package = %q, want ./cmd/api", svc.Package)
	}
	if svc.Directory != "/proj/cmd/api" {
		t.Errorf("Directory = %q, want /proj/cmd/api (derived from path)", svc.Directory)
	}
	if !svc.HotReload {
		t.Errorf("a path-defined Go service should default HotReload to true")
	}
	if !svc.AutoStart || !svc.AutoRestart {
		t.Errorf("should default AutoStart/AutoRestart to true like any other standalone service, got %+v", svc)
	}
}

func TestMergeStandaloneServiceBothPathAndCommandErrors(t *testing.T) {
	cfg := &File{Services: map[string]ServiceConfig{
		"api": {Path: "./cmd/api", Command: []string{"echo", "hi"}},
	}}
	if _, err := Merge("/proj", nil, cfg); err == nil {
		t.Fatal("expected an error when both path and command are set")
	}
}

func TestMergeStandaloneServiceDefaultsDirectoryToProjectRoot(t *testing.T) {
	cfg := &File{Services: map[string]ServiceConfig{
		"web": {Command: []string{"node", "server.js"}},
	}}
	got, err := Merge("/proj", nil, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got[0].Directory != "/proj" {
		t.Errorf("Directory = %q, want /proj", got[0].Directory)
	}
}

// TestLoadPreservesDeclarationOrder guards against the actual bug this
// was written to fix: yaml.v3 decoding "services" straight into a Go
// map loses all track of how the file was written, and Go map
// iteration order is deliberately randomized - so without
// File.UnmarshalYAML's explicit node walk, this order would be
// different (at random) on every single run instead of matching the
// file.
func TestLoadPreservesDeclarationOrder(t *testing.T) {
	root := t.TempDir()
	yaml := `
services:
  zebra:
    command: ["echo", "zebra"]
  apple:
    command: ["echo", "apple"]
  mango:
    command: ["echo", "mango"]
  banana:
    command: ["echo", "banana"]
`
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(yaml), 0o644); err != nil {
		t.Fatalf("writing %s: %v", FileName, err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantOrder := []string{"zebra", "apple", "mango", "banana"}
	if len(cfg.Order) != len(wantOrder) {
		t.Fatalf("cfg.Order = %v, want %v", cfg.Order, wantOrder)
	}
	for i, name := range wantOrder {
		if cfg.Order[i] != name {
			t.Errorf("cfg.Order[%d] = %q, want %q (full: %v)", i, cfg.Order[i], name, cfg.Order)
		}
	}

	got, err := Merge(root, nil, cfg)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d services, want %d", len(got), len(wantOrder))
	}
	for i, name := range wantOrder {
		if got[i].Name != name {
			t.Errorf("got[%d].Name = %q, want %q (declaration order, not map order)", i, got[i].Name, name)
		}
	}
}

func TestServiceOrderFallsBackToSortedWhenOrderUnset(t *testing.T) {
	// A File built directly (as every other test in this file does),
	// not through Load, has no Order - Merge must still be
	// deterministic (sorted), never raw map iteration order.
	cfg := &File{Services: map[string]ServiceConfig{
		"zebra": {Command: []string{"echo", "z"}},
		"apple": {Command: []string{"echo", "a"}},
		"mango": {Command: []string{"echo", "m"}},
	}}
	want := []string{"apple", "mango", "zebra"}
	for i := 0; i < 5; i++ {
		got, err := Merge("/proj", nil, cfg)
		if err != nil {
			t.Fatalf("Merge: %v", err)
		}
		for j, name := range want {
			if got[j].Name != name {
				t.Fatalf("iteration %d: got[%d].Name = %q, want %q", i, j, got[j].Name, name)
			}
		}
	}
}
