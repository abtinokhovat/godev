package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestColonEntersCommandMode(t *testing.T) {
	m, _ := newFakeGroupModel()

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	got := next.(Model)

	if !got.commandMode {
		t.Fatal("expected commandMode = true after \":\"")
	}
	if got.commandInput != "" {
		t.Errorf("commandInput = %q, want empty on entry", got.commandInput)
	}
}

func TestCommandModeTypingDoesNotTriggerNormalKeys(t *testing.T) {
	m, src := newFakeGroupModel()
	m.commandMode = true
	m.selected = 1 // api, currently Running per newFakeGroupModel

	// "s" would normally toggle start/stop on the selected service -
	// while typing a command, it must only be appended as text.
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	got := next.(Model)

	if got.commandInput != "s" {
		t.Fatalf("commandInput = %q, want %q", got.commandInput, "s")
	}
	if len(src.started) != 0 || len(src.stopped) != 0 {
		t.Errorf("typing inside the command prompt must not dispatch Start/Stop, got started=%v stopped=%v", src.started, src.stopped)
	}
}

func TestCommandModeBackspaceRemovesLastRune(t *testing.T) {
	m, _ := newFakeGroupModel()
	m.commandMode = true
	m.commandInput = "web"

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	got := next.(Model)
	if got.commandInput != "we" {
		t.Fatalf("commandInput after backspace = %q, want %q", got.commandInput, "we")
	}
}

func TestCommandModeEscCancels(t *testing.T) {
	m, _ := newFakeGroupModel()
	m.commandMode = true
	m.commandInput = "core"

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(Model)
	if got.commandMode {
		t.Error("expected commandMode = false after esc")
	}
	if got.commandInput != "" {
		t.Errorf("commandInput after esc = %q, want empty", got.commandInput)
	}
}

func TestCommandModeEnterStartsResolvedTargets(t *testing.T) {
	m, src := newFakeGroupModel()
	m.commandMode = true
	m.commandInput = "core web"

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.commandMode {
		t.Error("expected commandMode = false after enter")
	}
	if len(src.started) != 1 {
		t.Fatalf("StartServices calls = %d, want 1", len(src.started))
	}
	names := src.started[0]
	want := []string{"api", "worker", "web"}
	if len(names) != len(want) {
		t.Fatalf("StartServices names = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("names[%d] = %q, want %q (full: %v)", i, names[i], n, names)
		}
	}

	found := false
	for _, l := range got.logLines {
		if strings.Contains(l.text, "starting") {
			found = true
		}
	}
	if !found {
		t.Error("expected a local confirmation log line mentioning \"starting\"")
	}
}

func TestCommandModeEnterWithUnknownTargetDoesNotStartAnything(t *testing.T) {
	m, src := newFakeGroupModel()
	m.commandMode = true
	m.commandInput = "nonexistent"

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.commandMode {
		t.Error("expected commandMode = false after enter, even on error")
	}
	if len(src.started) != 0 {
		t.Errorf("StartServices should not have been called for an unmatched target, got %v", src.started)
	}
	found := false
	for _, l := range got.logLines {
		if strings.Contains(l.text, "nonexistent") {
			found = true
		}
	}
	if !found {
		t.Error("expected a local log line reporting the unmatched target")
	}
}

func TestCommandModeEnterWithEmptyInputIsNoop(t *testing.T) {
	m, src := newFakeGroupModel()
	m.commandMode = true
	m.commandInput = "   "

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.commandMode {
		t.Error("expected commandMode = false after enter")
	}
	if len(src.started) != 0 {
		t.Errorf("StartServices should not have been called for blank input, got %v", src.started)
	}
}

func TestCommandModeSpaceAndRunesAccumulate(t *testing.T) {
	m, _ := newFakeGroupModel()
	m.commandMode = true

	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("c")},
		{Type: tea.KeyRunes, Runes: []rune("o")},
		{Type: tea.KeyRunes, Runes: []rune("r")},
		{Type: tea.KeyRunes, Runes: []rune("e")},
		{Type: tea.KeySpace},
		{Type: tea.KeyRunes, Runes: []rune("w")},
	} {
		next, _ := m.handleKey(msg)
		m = next.(Model)
	}

	if m.commandInput != "core w" {
		t.Fatalf("commandInput = %q, want %q", m.commandInput, "core w")
	}
}
