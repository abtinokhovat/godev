package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/abtinokhovat/godev/internal/config"
	"github.com/abtinokhovat/godev/internal/discovery"
	"github.com/abtinokhovat/godev/internal/discovery/jetbrains"
)

// candidate is one thing `godev init` found that could become a
// .godev.yaml entry: a discovered Go package, or a JetBrains-imported
// non-Go run configuration. A JetBrains Go run configuration never
// becomes its own candidate - it enriches a matching Go candidate's
// Args/Env/Group instead, exactly like it enriches an already-
// discovered service at runtime today.
type candidate struct {
	Name      string
	IsGo      bool
	Package   string   // Go import path, set when IsGo
	Command   []string // explicit command, set when !IsGo
	Directory string   // absolute
	Args      []string
	Env       map[string]string
	Group     []string
}

func (c candidate) source() string {
	if c.IsGo {
		return c.Package
	}
	return strings.Join(c.Command, " ")
}

func (c candidate) kind() string {
	if c.IsGo {
		return "go"
	}
	return "command"
}

// gatherCandidates runs Go discovery and JetBrains import exactly
// once and normalizes the result into a flat, selectable list,
// skipping any name already present in existingNames - a re-run of
// `godev init` only ever offers what's genuinely new, never touching
// services the user already configured (or already declined).
func gatherCandidates(root string, isGoModule bool, existingNames map[string]bool) ([]candidate, error) {
	var candidates []candidate
	byDir := map[string]int{}

	if isGoModule {
		apps, err := discovery.Discover(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: Go package discovery failed, continuing without auto-discovered Go services: %v\n", err)
		}
		for _, a := range apps {
			if existingNames[a.Name] {
				continue
			}
			byDir[a.Directory] = len(candidates)
			candidates = append(candidates, candidate{
				Name: a.Name, IsGo: true, Package: a.Package, Directory: a.Directory,
			})
		}
	}

	configs, err := jetbrains.Import(root)
	if err != nil {
		return nil, fmt.Errorf("importing JetBrains run configurations: %w", err)
	}

	used := map[string]bool{}
	for n := range existingNames {
		used[n] = true
	}
	for _, c := range candidates {
		used[c.Name] = true
	}

	for _, rc := range configs {
		if rc.IsGo {
			i, ok := byDir[rc.Directory]
			if !ok {
				// No discovered (and not-yet-configured) Go candidate
				// at this directory - either already configured, or
				// not a service godev's own discovery found.
				continue
			}
			candidates[i].Args = rc.Args
			candidates[i].Env = rc.Env
			candidates[i].Group = rc.Group
			continue
		}
		if existingNames[rc.Name] {
			continue
		}
		name := rc.Name
		for used[name] {
			name += "-2"
		}
		used[name] = true
		candidates = append(candidates, candidate{
			Name: name, IsGo: false, Command: rc.Command, Directory: rc.Directory,
			Env: rc.Env, Group: rc.Group,
		})
	}

	// Go candidates first, JetBrains-imported command candidates after
	// - stable within each group (discovery/import order).
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].IsGo && !candidates[j].IsGo
	})

	return candidates, nil
}

// writeSelected merges the selected/renamed candidates into
// projectRoot's .godev.yaml, creating the file if it doesn't exist
// yet, and preserving every entry already there untouched. Every
// written entry gets auto_start: false explicitly - `godev init`
// only ever decides what *exists*, never what runs by default. That's
// a deliberate, separate choice the user makes afterward by hand-
// editing .godev.yaml, or by starting things explicitly (`godev run
// <target>`, or the TUI's start key) - never an automatic side effect
// of curating the list, since some of what's curated here (an
// imported shell script, in particular) can be destructive to run
// without ever having been reviewed.
func writeSelected(root string, existing *config.File, selected []candidate) error {
	f := existing
	if f == nil {
		f = &config.File{}
	}
	if f.Services == nil {
		f.Services = map[string]config.ServiceConfig{}
	}
	falseVal, trueVal := false, true
	for _, c := range selected {
		sc := config.ServiceConfig{
			Args:        c.Args,
			Env:         c.Env,
			Group:       c.Group,
			AutoStart:   &falseVal,
			AutoRestart: &trueVal,
		}
		if c.IsGo {
			sc.Path = c.Package
			sc.HotReload = &trueVal
		} else {
			sc.Command = c.Command
			sc.Directory = relDirectory(root, c.Directory)
		}
		f.Services[c.Name] = sc
	}
	return writeConfig(filepath.Join(root, config.FileName), *f)
}

// relDirectory renders dir relative to root for a cleaner, more
// portable .godev.yaml when possible, falling back to the absolute
// path for anything outside the project tree.
func relDirectory(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return dir
	}
	if rel == "." {
		return ""
	}
	return rel
}

// runInitFlow discovers candidates, runs the interactive checklist,
// and writes whatever the user selects into .godev.yaml. It's a
// no-op (wrote=false, no error) when there's nothing new to offer, or
// when the user selects nothing / cancels.
func runInitFlow(root string, isGoModule bool) (wrote bool, err error) {
	existing, err := config.Load(root)
	if err != nil {
		return false, fmt.Errorf("loading %s: %w", config.FileName, err)
	}
	existingNames := map[string]bool{}
	if existing != nil {
		for name := range existing.Services {
			existingNames[name] = true
		}
	}

	candidates, err := gatherCandidates(root, isGoModule, existingNames)
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		if len(existingNames) > 0 {
			fmt.Println("Nothing new to add - every discovered Go package and JetBrains run configuration is already in .godev.yaml.")
		} else {
			fmt.Println("Nothing discovered: no Go main packages and no JetBrains run configurations found.")
			fmt.Printf("Define services by hand in %s instead - see .godev.example.yaml.\n", config.FileName)
		}
		return false, nil
	}

	selected, ok := runInitMenu(candidates, existingNames)
	if !ok {
		fmt.Println("Cancelled - nothing written.")
		return false, nil
	}
	if len(selected) == 0 {
		fmt.Println("Nothing selected - nothing written.")
		return false, nil
	}

	if err := writeSelected(root, existing, selected); err != nil {
		return false, fmt.Errorf("writing %s: %w", config.FileName, err)
	}

	fmt.Printf("Added %d service(s) to %s (auto_start: false - start them with `godev run <name>` or from the TUI):\n", len(selected), config.FileName)
	for _, c := range selected {
		fmt.Printf("  %-16s %s: %s\n", c.Name, c.kind(), c.source())
	}
	return true, nil
}

// --- interactive checklist ---

type initItem struct {
	candidate
	selected bool
	name     string // editable copy of candidate.Name
}

type initMenuModel struct {
	items         []initItem
	cursor        int
	renaming      bool
	renameBuf     string
	existingNames map[string]bool
	errMsg        string
	confirmed     bool
	cancelled     bool
	width         int
}

func runInitMenu(candidates []candidate, existingNames map[string]bool) ([]candidate, bool) {
	items := make([]initItem, len(candidates))
	for i, c := range candidates {
		items[i] = initItem{candidate: c, name: c.Name}
	}
	m := initMenuModel{items: items, existingNames: existingNames}
	program := tea.NewProgram(m, tea.WithMouseCellMotion())
	final, err := program.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return nil, false
	}
	fm := final.(initMenuModel)
	if fm.cancelled || !fm.confirmed {
		return nil, false
	}
	var out []candidate
	for _, it := range fm.items {
		if !it.selected {
			continue
		}
		c := it.candidate
		c.Name = it.name
		out = append(out, c)
	}
	return out, true
}

func (m initMenuModel) Init() tea.Cmd { return nil }

func (m initMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if winMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = winMsg.Width
		return m, nil
	}

	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		return m.handleMouse(tea.MouseEvent(mouseMsg))
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.renaming {
		return m.updateRenaming(keyMsg)
	}

	switch keyMsg.String() {
	case "q", "ctrl+c", "esc":
		m.cancelled = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.errMsg = ""
	case "down", "j":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		m.errMsg = ""
	case " ", "x":
		if len(m.items) > 0 {
			m.items[m.cursor].selected = !m.items[m.cursor].selected
		}
		m.errMsg = ""
	case "a":
		allSelected := true
		for _, it := range m.items {
			if !it.selected {
				allSelected = false
				break
			}
		}
		for i := range m.items {
			m.items[i].selected = !allSelected
		}
		m.errMsg = ""
	case "r":
		if len(m.items) > 0 {
			m.renaming = true
			m.renameBuf = m.items[m.cursor].name
			m.errMsg = ""
		}
	case "enter":
		m.confirmed = true
		return m, tea.Quit
	}
	return m, nil
}

// handleMouse lets a left click do what space+the cursor would: the
// header (y=0), the blank line under it (y=1), and any click while
// renaming are all no-ops - items start at y=2, one screen line each.
func (m initMenuModel) handleMouse(ev tea.MouseEvent) (tea.Model, tea.Cmd) {
	if m.renaming || ev.Button != tea.MouseButtonLeft || ev.Action != tea.MouseActionPress {
		return m, nil
	}
	idx := ev.Y - 2
	if idx < 0 || idx >= len(m.items) {
		return m, nil
	}
	m.cursor = idx
	m.items[idx].selected = !m.items[idx].selected
	m.errMsg = ""
	return m, nil
}

func (m initMenuModel) updateRenaming(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.renaming = false
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.renameBuf)
		if name == "" {
			m.errMsg = "name can't be empty"
			return m, nil
		}
		if m.existingNames[name] {
			m.errMsg = fmt.Sprintf("%q is already in .godev.yaml", name)
			return m, nil
		}
		for i, it := range m.items {
			if i != m.cursor && it.name == name {
				m.errMsg = fmt.Sprintf("%q is already used by another selected item", name)
				return m, nil
			}
		}
		m.items[m.cursor].name = name
		m.renaming = false
		m.errMsg = ""
		return m, nil
	case "backspace":
		if len(m.renameBuf) > 0 {
			r := []rune(m.renameBuf)
			m.renameBuf = string(r[:len(r)-1])
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.renameBuf += string(msg.Runes)
		}
		return m, nil
	}
}

var (
	initMenuStyleSection  = lipgloss.NewStyle().Bold(true)
	initMenuStyleDim      = lipgloss.NewStyle().Faint(true)
	initMenuStyleSelected = lipgloss.NewStyle().Reverse(true)
	initMenuStyleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

func (m initMenuModel) View() string {
	var b strings.Builder
	b.WriteString(initMenuStyleSection.Render("godev init") + " " + initMenuStyleDim.Render("· select services to add to .godev.yaml") + "\n\n")

	for i, it := range m.items {
		box := "[ ]"
		if it.selected {
			box = "[x]"
		}
		line := fmt.Sprintf("%s %-20s %-8s %s", box, truncateMenu(it.name, 20), it.kind(), it.source())
		if i == m.cursor && m.renaming {
			line = fmt.Sprintf("%s %-20s %-8s %s", box, truncateMenu(m.renameBuf+"█", 20), it.kind(), it.source())
		}
		if i == m.cursor {
			b.WriteString(initMenuStyleSelected.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(initMenuStyleErr.Render(m.errMsg) + "\n\n")
	}
	if m.renaming {
		b.WriteString(initMenuStyleDim.Render("type a new name · enter confirm · esc cancel rename"))
	} else {
		b.WriteString(initMenuStyleDim.Render("↑↓ move   space select   a select all/none   r rename   enter confirm and write   q cancel"))
	}
	b.WriteString("\n")
	return b.String()
}

func truncateMenu(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}
