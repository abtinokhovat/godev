package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/abtinokhovat/godev/internal/logs"
)

// selectionTestModel is scrollableTestModel with distinguishable,
// short log lines (rather than 40 copies of "line") so selection
// boundaries and copied text are actually checkable.
func selectionTestModel(t *testing.T) Model {
	t.Helper()
	m := scrollableTestModel(t)
	m.view = ViewLogs
	m.logLines = nil
	// More lines than the test terminal's content height (26 rows, per
	// scrollableTestModel's 100x30), so m.scroll actually has room to
	// move - a selection near the top/bottom edge only means anything
	// once there's more content than fits on screen at once.
	for i := 0; i < 60; i++ {
		m.logLines = append(m.logLines, logLine{
			service: "api", stream: logs.StreamStdout, time: time.Now(),
			text: "line " + string(rune('a'+(i%26))),
		})
	}
	return m
}

// contentTopY is the screen row of the first visible log line in
// selectionTestModel's layout: header (row 0) + title + blank (2 more
// rows within the content pane) = row 3.
const contentTopY = 3

func contentLeftX(m Model) int {
	return m.sidebarWidth() + 3
}

func TestLogsHitTestFindsAbsoluteLineAndColumn(t *testing.T) {
	m := selectionTestModel(t)
	left := contentLeftX(m)

	line, col, ok := m.logsHitTest(left+5, contentTopY)
	if !ok {
		t.Fatal("expected a hit on the first visible log row")
	}
	all := m.renderContent(m.contentWidth())
	wantLine := m.logsWindowStart(len(all))
	if line != wantLine {
		t.Errorf("line = %d, want %d (top visible line)", line, wantLine)
	}
	if col != 5 {
		t.Errorf("col = %d, want 5", col)
	}
}

func TestLogsHitTestMissesSidebarAndHeader(t *testing.T) {
	m := selectionTestModel(t)

	if _, _, ok := m.logsHitTest(2, contentTopY); ok {
		t.Error("expected no hit over the sidebar column")
	}
	if _, _, ok := m.logsHitTest(contentLeftX(m)+2, 0); ok {
		t.Error("expected no hit on the header row")
	}
	if _, _, ok := m.logsHitTest(contentLeftX(m)+2, 1); ok {
		t.Error("expected no hit on the title row")
	}
}

func TestDragSelectSameLineCopiesSubstring(t *testing.T) {
	m := selectionTestModel(t)
	left := contentLeftX(m)

	next, _ := m.handleMouse(tea.MouseEvent{X: left, Y: contentTopY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	if !m.selecting {
		t.Fatal("expected selecting = true after press on a log line")
	}

	next, _ = m.handleMouse(tea.MouseEvent{X: left + 4, Y: contentTopY, Action: tea.MouseActionMotion})
	m = next.(Model)

	next, _ = m.handleMouse(tea.MouseEvent{X: left + 4, Y: contentTopY, Action: tea.MouseActionRelease})
	m = next.(Model)

	if m.selecting {
		t.Error("expected selecting = false after release")
	}
	last := m.logLines[len(m.logLines)-1]
	if !strings.Contains(last.text, "copied to clipboard") {
		t.Fatalf("expected a copy confirmation log line, got %+v", last)
	}
}

func TestDragSelectMultiLineJoinsWithNewline(t *testing.T) {
	m := selectionTestModel(t)
	left := contentLeftX(m)

	next, _ := m.handleMouse(tea.MouseEvent{X: left, Y: contentTopY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)

	next, _ = m.handleMouse(tea.MouseEvent{X: left + 6, Y: contentTopY + 2, Action: tea.MouseActionMotion})
	m = next.(Model)

	fromLine, _, toLine, _ := orderSelection(m.selAnchorLine, m.selAnchorCol, m.selCursorLine, m.selCursorCol)
	if toLine != fromLine+2 {
		t.Fatalf("expected the selection to span 3 lines, got fromLine=%d toLine=%d", fromLine, toLine)
	}

	text := m.selectedText()
	if got := strings.Count(text, "\n"); got != 2 {
		t.Fatalf("selectedText() = %q, want 2 newlines (3 lines)", text)
	}
}

func TestDragAboveTopEdgeScrollsUpAndTracksTopLine(t *testing.T) {
	m := selectionTestModel(t)
	left := contentLeftX(m)
	m.scroll = 3

	next, _ := m.handleMouse(tea.MouseEvent{X: left, Y: contentTopY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	startScroll := m.scroll

	// Drag above the content pane's first log row (into the title/blank
	// rows) - should scroll up by one and pin the cursor to whatever
	// line that reveals at the top.
	next, cmd := m.handleMouse(tea.MouseEvent{X: left + 2, Y: contentTopY - 1, Action: tea.MouseActionMotion})
	m = next.(Model)

	if m.scroll != startScroll+1 {
		t.Fatalf("scroll = %d, want %d (one step up)", m.scroll, startScroll+1)
	}
	if m.selDragEdge != -1 {
		t.Errorf("selDragEdge = %d, want -1 (above pane)", m.selDragEdge)
	}
	if cmd == nil {
		t.Error("expected a dragScrollTick command to be scheduled on entering the edge")
	}
	all := m.renderContent(m.contentWidth())
	if want := m.logsWindowStart(len(all)); m.selCursorLine != want {
		t.Errorf("selCursorLine = %d, want %d (new top line)", m.selCursorLine, want)
	}
}

func TestAdvanceEdgeScrollContinuesWithoutFurtherMotion(t *testing.T) {
	m := selectionTestModel(t)
	m.selecting = true
	m.selDragEdge = -1
	m.scroll = 5
	before := m.scroll

	m = m.advanceEdgeScroll()

	if m.scroll != before+1 {
		t.Fatalf("scroll after advanceEdgeScroll = %d, want %d", m.scroll, before+1)
	}
}

func TestReleaseWithNoDragDoesNotCopyEmptySelection(t *testing.T) {
	m := selectionTestModel(t)
	left := contentLeftX(m)

	next, _ := m.handleMouse(tea.MouseEvent{X: left, Y: contentTopY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	before := len(m.logLines)

	next, _ = m.handleMouse(tea.MouseEvent{X: left, Y: contentTopY, Action: tea.MouseActionRelease})
	m = next.(Model)

	if len(m.logLines) != before {
		t.Errorf("a zero-length selection should not append a copy confirmation, logLines went from %d to %d", before, len(m.logLines))
	}
}

func TestSelectedTextEmptyOutsideLogsView(t *testing.T) {
	m := selectionTestModel(t)
	m.selecting = true
	m.selAnchorLine, m.selCursorLine = 0, 2
	m.view = ViewBuild

	if got := m.selectedText(); got != "" {
		t.Errorf("selectedText() outside ViewLogs = %q, want empty", got)
	}
}

func TestHighlightRangeWrapsWithReverseVideoNotFullReset(t *testing.T) {
	line := "hello world"
	got := highlightRange(line, 0, 5)
	if !strings.Contains(got, "\x1b[7m") || !strings.Contains(got, "\x1b[27m") {
		t.Fatalf("highlightRange output = %q, want SGR 7/27 wrapping", got)
	}
	if strings.Contains(got, "\x1b[0m") {
		t.Errorf("highlightRange should not use a full reset (would clobber existing color), got %q", got)
	}
}

// simulateReverse walks s tracking SGR reverse-video (7/27) state the
// way a real terminal would, and returns which visible (non-escape)
// characters were actually rendered reversed - the closest thing to
// "what does this look like on screen" a unit test can check.
func simulateReverse(s string) string {
	reverseOn := false
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			end := strings.IndexByte(s[i:], 'm')
			if end < 0 {
				break
			}
			switch s[i : i+end+1] {
			case "\x1b[7m":
				reverseOn = true
			case "\x1b[27m", "\x1b[0m", "\x1b[m":
				reverseOn = false
			}
			i += end + 1
			continue
		}
		if reverseOn {
			out.WriteByte(s[i])
		}
		i++
	}
	return out.String()
}

// TestHighlightRangeSurvivesEmbeddedResets is a regression test for a
// real bug: a log line is never one uninterrupted style run - the
// timestamp, [service] tag, and message text are each their own
// lipgloss Render() call, and each one ends with a *full* SGR reset
// (\x1b[0m), not a scoped "undo my color" code. That reset cancels
// highlightRange's own reverse-video wrapper the instant it fires, so
// only whichever segment came first (the timestamp) actually looked
// selected on screen - every later segment's own reset silently
// turned the highlight back off again. Caught via a real user report
// ("only the timestamps get highlighted"), not by the earlier,
// single-style-run version of this test above.
func TestHighlightRangeSurvivesEmbeddedResets(t *testing.T) {
	ts := "\x1b[38;5;240m15:04:05\x1b[0m"
	tag := "\x1b[1;38;5;214m[api]\x1b[0m"
	line := ts + " " + tag + " " + "some log message text here"

	highlighted := highlightRange(line, 0, 30)

	wantPlain := ansi.Strip(line)
	if len(wantPlain) < 30 {
		t.Fatalf("test line too short: %q", wantPlain)
	}
	want := wantPlain[:30]
	if got := simulateReverse(highlighted); got != want {
		t.Fatalf("reversed-visible text = %q, want %q (highlight should span the whole range across every styled segment, not just the first)", got, want)
	}
}
