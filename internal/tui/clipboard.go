package tui

import (
	"encoding/base64"
	"fmt"
	"os"
)

// maxClipboardBytes caps what osc52Copy actually sends: some
// terminals (xterm by default) cap an OSC 52 payload around 100000
// bytes and simply drop anything larger, so copying only the tail of
// a huge selection is far more useful than silently copying nothing
// at all past that limit.
const maxClipboardBytes = 74000

// osc52Copy writes an OSC 52 "set clipboard" escape sequence directly
// to stdout - called automatically when a log-pane drag-select
// completes (see selection.go), the same way releasing a mouse
// selection sets the clipboard in a normal terminal. Every terminal
// this TUI already depends on for mouse support (and most others,
// plus tmux/screen with clipboard passthrough enabled) intercepts and
// acts on this without it ever appearing on screen, which is what
// lets it work identically over SSH, inside tmux, or on a bare local
// terminal, with no OS clipboard API and no extra dependency.
func osc52Copy(text string) {
	if len(text) > maxClipboardBytes {
		text = text[len(text)-maxClipboardBytes:]
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", encoded)
}
