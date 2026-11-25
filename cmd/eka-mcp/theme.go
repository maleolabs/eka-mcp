package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
)

// This file is the shared presentation theme of the eka-mcp human
// commands (banner, configure, install). It mirrors the eka-cli visual
// language (cmd/ui) so both binaries read as one product:
//
//   - global left margin: every non-blank human line is indented two
//     spaces (eka-cli Margin "  "); blank lines stay truly blank;
//   - 256-color palette: info 75, success 114, warning 214, error 167,
//     progress 80, dim 245, headings in info (accent = info);
//   - section blocks: accent title + aligned key-value rows under the
//     └── tree glyph (glyph dim, label info, value plain);
//   - colors only on TTY with NO_COLOR unset and TERM != dumb;
//     piped output is plain UTF-8, byte-deterministic.

// themeMargin is the uniform left margin of the output container, in
// columns — identical to eka-cli's Margin.
const themeMargin = "  "

// SGR palette — identical codes to eka-cli cmd/ui.
const (
	themeInfo     = "38;5;75"
	themeSuccess  = "38;5;114"
	themeWarning  = "38;5;214"
	themeError    = "38;5;167"
	themeProgress = "38;5;80"
	themeDim      = "38;5;245"
	themeAccent   = themeInfo

	themeTreeLast = "└──"
)

// theme glyphs for the interactive selectors (bubbletea views).
const (
	glyphSingleOn  = "◉"
	glyphSingleOff = "○"
	glyphMultiOn   = "■"
	glyphMultiOff  = "□"
	glyphCursor    = "›"
)

// theme is the presentation context of one human command execution.
type theme struct {
	Color bool
	W     io.Writer
}

// newTheme builds the theme against w: colors follow the shared TTY +
// NO_COLOR + TERM rule, and W is the margin-indenting writer so human
// output never sticks to the terminal's left edge.
func newTheme(w io.Writer) *theme {
	return &theme{Color: themeColorEnabled(w), W: newThemeMarginWriter(w)}
}

// paint wraps text in the SGR code when colors are enabled.
func (t *theme) paint(code, text string) string {
	if !t.Color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (t *theme) info(text string) string     { return t.paint(themeInfo, text) }
func (t *theme) success(text string) string  { return t.paint(themeSuccess, text) }
func (t *theme) warning(text string) string  { return t.paint(themeWarning, text) }
func (t *theme) failure(text string) string  { return t.paint(themeError, text) }
func (t *theme) progress(text string) string { return t.paint(themeProgress, text) }
func (t *theme) dim(text string) string      { return t.paint(themeDim, text) }
func (t *theme) accent(text string) string   { return t.paint(themeAccent, text) }

// section prints one titled block: accent title, blank-separated,
// aligned key-value rows under the dim └── glyph.
func (t *theme) section(title string, rows [][2]string) {
	fmt.Fprintln(t.W, t.accent(title))
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(t.W, "%s %s   %s\n", t.dim(themeTreeLast), t.info(fmt.Sprintf("%-*s", width, r[0])), r[1])
	}
}

// blank prints one truly blank line (the margin writer emits no indent
// for empty lines).
func (t *theme) blank() { fmt.Fprintln(t.W, "") }

// themeColorEnabled mirrors the shared rule: colors only on a TTY with
// NO_COLOR unset and TERM != dumb.
func themeColorEnabled(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	if !isatty.IsTerminal(f.Fd()) {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}

// themeMarginWriter indents every non-blank line with themeMargin. A
// write starting with \r is a redraw and passes through untouched.
type themeMarginWriter struct {
	w           io.Writer
	atLineStart bool
}

// newThemeMarginWriter wraps w: the stream starts at a fresh line, so
// the first non-blank line is margined (the zero value would wrongly
// skip it — always construct through here).
func newThemeMarginWriter(w io.Writer) *themeMarginWriter {
	return &themeMarginWriter{w: w, atLineStart: true}
}

func (m *themeMarginWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if p[0] == '\r' {
		return m.w.Write(p)
	}
	var out []byte
	atStart := m.atLineStart
	for i := 0; i < len(p); i++ {
		if atStart && p[i] != '\n' {
			out = append(out, themeMargin...)
		}
		out = append(out, p[i])
		atStart = p[i] == '\n'
	}
	m.atLineStart = atStart
	if _, err := m.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}
