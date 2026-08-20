package config

import (
	"testing"

	"github.com/abtinokhovat/godev/internal/domain"
)

func TestMergeNilConfigIsNoop(t *testing.T) {
	services := []domain.Service{{Name: "api", AutoStart: true}}
	got := Merge(services, nil)
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
		},
	}}

	got := Merge(services, cfg)

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

	if !worker.AutoStart {
		t.Errorf("worker should be untouched by api's overrides")
	}
}

func TestMergePreservesUnknownServiceUnchanged(t *testing.T) {
	services := []domain.Service{{Name: "api", Args: []string{"orig"}}}
	cfg := &File{Services: map[string]ServiceConfig{
		"nonexistent": {Args: []string{"new"}},
	}}
	got := Merge(services, cfg)
	if len(got[0].Args) != 1 || got[0].Args[0] != "orig" {
		t.Fatalf("api.Args should be unchanged, got %v", got[0].Args)
	}
}
