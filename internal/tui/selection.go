package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// dragScrollEdgeInterval is how often the drag continues auto-scrolling
// while the mouse sits above/below the log pane's visible rows with
// the button still held, rather than only advancing when the mouse
// actually moves - matching a normal terminal's "hold near the edge
// to keep scrolling" selection behavior.
const dragScrollEdgeInterval = 80 * time.Millisecond

type dragScrollTickMsg struct{}

func dragScrollTick() tea.Cmd {
	return tea.Tick(dragScrollEdgeInterval, func(time.Time) tea.Msg { return dragScrollTickMsg{} })
}

// logsWindowStart is the absolute index (into a Logs-view content
// list of length total) of the first line currently visible, given
// m.scroll - the same arithmetic window() applies, exposed here so
// hit-testing and drag handling agree with what's actually on screen.
func (m Model) logsWindowStart(total int) int {
	h := m.contentHeight()
	start := total - m.scroll - h
	if start < 0 {
		start = 0
	}
	return start
}

// logsHitTest converts a screen coordinate into an absolute Logs
// content-line index plus a column in that line's plain text, only
// when (x,y) actually lands on a rendered log line - used to decide
// whether a press starts a selection at all. Mirrors the row math
// serviceAtScreenPos uses for the sidebar: row 0 is the header, then
// within the content pane the title and a blank line precede the log
// lines themselves.
func (m Model) logsHitTest(x, y int) (line, col int, ok bool) {
	if m.view != ViewLogs {
		return 0, 0, false
	}
	left := m.sidebarWidth() + 3
	if x < left {
		return 0, 0, false
	}
	logRow := (y - 1) - 2
	h := m.contentHeight()
	if logRow < 0 || logRow >= h {
		return 0, 0, false
	}
	all := m.renderContent(m.contentWidth())
	idx := m.logsWindowStart(len(all)) + logRow
	if idx < 0 || idx >= len(all) {
		return 0, 0, false
	}
	return idx, x - left + m.hScroll, true
}

// clampIdx bounds idx to a valid index into a slice of length n (0 if
// n is 0), for the "clamp to nearest visible line" behavior a drag
// needs when it's pointing above/below the pane rather than exactly
// on a line.
func clampIdx(idx, n int) int {
	if n == 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

// updateDrag extends the in-progress selection to (x,y), auto-scrolling
// the log pane when the point is above or below its visible rows so
// dragging toward either edge keeps revealing more content - the same
// gesture a normal terminal's own text selection supports, which
// alt-screen mode's lack of real scrollback otherwise rules out.
func (m Model) updateDrag(x, y int) (Model, tea.Cmd) {
	if m.view != ViewLogs {
		m.selecting = false
		m.selDragEdge = 0
		return m, nil
	}

	left := m.sidebarWidth() + 3
	col := x - left + m.hScroll
	if col < 0 {
		col = 0
	}

	all := m.renderContent(m.contentWidth())
	h := m.contentHeight()
	logRow := (y - 1) - 2

	prevEdge := m.selDragEdge
	switch {
	case logRow < 0:
		// Scroll first, then read the line that placed at the top -
		// otherwise the cursor would point at the line that was
		// already at top before this step, one behind what actually
		// became newly visible.
		m.selDragEdge = -1
		m.scroll++
		m.clampScroll()
		m.selCursorLine = m.logsWindowStart(len(all))
		m.selCursorCol = col
	case logRow >= h:
		m.selDragEdge = 1
		m.scroll--
		if m.scroll < 0 {
			m.scroll = 0
		}
		m.selCursorLine = clampIdx(m.logsWindowStart(len(all))+h-1, len(all))
		m.selCursorCol = col
	default:
		m.selDragEdge = 0
		m.selCursorLine = clampIdx(m.logsWindowStart(len(all))+logRow, len(all))
		m.selCursorCol = col
	}

	if m.selDragEdge != 0 && prevEdge == 0 {
		return m, dragScrollTick()
	}
	return m, nil
}

// advanceEdgeScroll performs one auto-scroll step while the drag point
// sits beyond the log pane's visible rows - the periodic half of the
// gesture updateDrag starts, continuing it even while the mouse itself
// stays still (as long as it's still positioned past the edge).
func (m Model) advanceEdgeScroll() Model {
	all := m.renderContent(m.contentWidth())
	h := m.contentHeight()
	if m.selDragEdge < 0 {
		m.scroll++
		m.clampScroll()
		m.selCursorLine = m.logsWindowStart(len(all))
	} else {
		m.scroll--
		if m.scroll < 0 {
			m.scroll = 0
		}
		m.selCursorLine = clampIdx(m.logsWindowStart(len(all))+h-1, len(all))
	}
	return m
}

// finishDrag ends the current selection, copying whatever text it
// covers to the clipboard - the drag-release equivalent of a normal
// terminal setting its selection buffer as soon as you let go of the
// mouse button, so there's no separate copy keystroke to remember.
func (m Model) finishDrag() (Model, tea.Cmd) {
	text := m.selectedText()
	m.selecting = false
	m.selDragEdge = 0
	if strings.TrimSpace(text) == "" {
		return m, nil
	}
	osc52Copy(text)
	n := strings.Count(text, "\n") + 1
	m.appendLocalLogLine(fmt.Sprintf("selected %d line(s), copied to clipboard", n))
	return m, nil
}

// orderSelection normalizes an anchor/cursor pair into an ordered
// (from, to) range regardless of which direction the drag went.
func orderSelection(aLine, aCol, cLine, cCol int) (fromLine, fromCol, toLine, toCol int) {
	if aLine < cLine || (aLine == cLine && aCol <= cCol) {
		return aLine, aCol, cLine, cCol
	}
	return cLine, cCol, aLine, aCol
}

// logsPlainLines is renderLogsContent's output with all ANSI styling
// stripped - the coordinate space selection columns are measured in,
// and what actually gets copied to the clipboard (raw escape codes
// have no business in whatever the clipboard lands in).
func (m Model) logsPlainLines() []string {
	styled := m.renderContent(m.contentWidth())
	out := make([]string, len(styled))
	for i, s := range styled {
		out[i] = ansi.Strip(s)
	}
	return out
}

// sliceCols returns the runes of s in [from, to), clamped to s's
// actual length - selection columns routinely run past a short
// line's end (the user dragged further right than the text goes),
// which should just mean "to the end of the line", not a panic.
func sliceCols(s string, from, to int) string {
	r := []rune(s)
	if from < 0 {
		from = 0
	}
	if from > len(r) {
		from = len(r)
	}
	if to > len(r) {
		to = len(r)
	}
	if to < from {
		to = from
	}
	return string(r[from:to])
}

// selectedText returns the plain text currently spanned by
// selAnchor/selCursor, joining multiple lines with "\n" - nil/empty
// once the view has moved away from Logs, since there's nothing
// sensible left to extract.
func (m Model) selectedText() string {
	if m.view != ViewLogs {
		return ""
	}
	fromLine, fromCol, toLine, toCol := orderSelection(m.selAnchorLine, m.selAnchorCol, m.selCursorLine, m.selCursorCol)
	lines := m.logsPlainLines()
	if fromLine < 0 || fromLine >= len(lines) || toLine < 0 || toLine >= len(lines) {
		return ""
	}
	if fromLine == toLine {
		return sliceCols(lines[fromLine], fromCol, toCol)
	}
	var b strings.Builder
	b.WriteString(sliceCols(lines[fromLine], fromCol, 1<<30))
	for i := fromLine + 1; i < toLine; i++ {
		b.WriteByte('\n')
		b.WriteString(lines[i])
	}
	b.WriteByte('\n')
	b.WriteString(sliceCols(lines[toLine], 0, toCol))
	return b.String()
}

// highlightRange wraps [from, to) of an ANSI-styled line in reverse
// video, using ansi.Cut (not a plain rune slice) so it doesn't break
// any escape codes already in the line - the same tool view.go already
// uses to safely cut a styled line for horizontal scroll. SGR 7/27
// (reverse on/off) rather than a full reset, so it doesn't clobber
// whatever color the line already had once the highlight ends.
//
// A log line is rarely one uninterrupted style run, though - the
// timestamp, the [service] tag, and the message are each their own
// lipgloss.Style.Render() call, and every one of those ends with its
// own full SGR reset (\x1b[0m), not just an "undo my color" code. Any
// such reset inside the highlighted range cancels our reverse-video
// wrapper the instant it fires, same as it would cancel anything
// else's styling - without re-asserting reverse after it, only
// whichever segment happens to come first (the timestamp) actually
// looks selected, with every later segment's own reset silently
// turning the highlight back off. Re-inserting \x1b[7m right after
// every reset found inside the cut segment keeps it applied across
// however many separately-styled runs the range spans.
func highlightRange(line string, from, to int) string {
	if to <= from {
		return line
	}
	mid := ansi.Cut(line, from, to)
	if mid == "" {
		return line
	}
	mid = strings.ReplaceAll(mid, "\x1b[0m", "\x1b[0m\x1b[7m")
	mid = strings.ReplaceAll(mid, "\x1b[m", "\x1b[m\x1b[7m")

	pre := ansi.Cut(line, 0, from)
	post := ansi.Cut(line, to, 1<<30)
	return pre + "\x1b[7m" + mid + "\x1b[27m" + post
}

// applySelectionHighlight overlays the live drag selection onto a
// Logs view's full content list, before it gets windowed down to the
// viewport - so the highlighted range stays aligned with the same
// absolute line indices selecting/updateDrag operate on regardless of
// scroll. A no-op outside an active Logs-pane drag.
func (m Model) applySelectionHighlight(lines []string) []string {
	if !m.selecting || m.view != ViewLogs {
		return lines
	}
	fromLine, fromCol, toLine, toCol := orderSelection(m.selAnchorLine, m.selAnchorCol, m.selCursorLine, m.selCursorCol)
	if fromLine >= len(lines) || toLine < 0 {
		return lines
	}
	out := make([]string, len(lines))
	copy(out, lines)
	for i := max(fromLine, 0); i <= toLine && i < len(lines); i++ {
		start := 0
		if i == fromLine {
			start = fromCol
		}
		end := 1 << 30
		if i == toLine {
			end = toCol
		}
		out[i] = highlightRange(lines[i], start, end)
	}
	return out
}
