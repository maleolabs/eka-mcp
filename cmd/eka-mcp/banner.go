package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/maleolabs/eka-mcp"
	"github.com/maleolabs/eka-mcp/internal/eka"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// bannerState is the state-derived data the banner renders. Every field
// is deterministic for a given store/plugin state — no wall-clock
// timestamps, no random ids.
type bannerState struct {
	// Runtime
	Version     string // eka-mcp version (pack.Version)
	PackName    string // embedded pack name (skills/manifest.yaml pack.name)
	PackVersion string // embedded pack version
	PackStatus  string // embedded pack status (stable, etc.)
	Protocol    string // MCP protocol version
	Transport   string // transport line

	// Workspace
	WorkspacePath string
	Initialized   bool
	Projects      int
	Repos         int

	// Capabilities
	ToolCount     int
	ResourceCount int
}

// collectBannerState builds the banner state from the opened capability.
// It never crashes on an uninitialized workspace — projects/repos are 0
// and Initialized is false, per req §3 decision 5. Any store error is
// propagated so the banner can degrade to a deterministic fallback.
func collectBannerState(cap *eka.Capability) (bannerState, error) {
	st := bannerState{
		Version:   pack.Version,
		Protocol:  mcp.ProtocolVersion,
		Transport: "stdio · JSON-RPC 2.0 · newline-delimited",
	}
	info, err := pack.ReadPackInfo()
	if err != nil {
		// Deterministic fallback: use plugin identity when pack manifest
		// cannot be read (embedded FS must contain it, but do not crash).
		st.PackName = pack.Name
		st.PackVersion = pack.Version
		st.PackStatus = "stable"
	} else {
		st.PackName = info.Name
		st.PackVersion = info.Version
		st.PackStatus = info.Status
	}
	if cap != nil {
		st.WorkspacePath = cap.Path()
		st.Initialized = cap.Exists()
		if pc, err := cap.ProjectCount(); err == nil {
			st.Projects = pc
		}
		if rc, err := cap.RepoCount(); err == nil {
			st.Repos = rc
		}
	}
	st.ToolCount = mcp.ToolCount()
	if rc, err := mcp.ResourceCount(); err == nil {
		st.ResourceCount = rc
	}
	return st, nil
}

// bannerStyle is the local presentation style — the eka CLI visual
// language reimplemented LOCALLY in eka-mcp without importing eka-cli.
// Colors are emitted only when colorEnabled is true (TTY + NO_COLOR
// unset + TERM != dumb); otherwise bytes are plain UTF-8.
type bannerStyle struct {
	Color bool
	W     io.Writer
}

const (
	colorInfo   = "38;5;75"
	colorDim    = "38;5;245"
	colorAccent = colorInfo

	treeLast = "└──"
)

func (s *bannerStyle) paint(code, text string) string {
	if !s.Color {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
func (s *bannerStyle) info(text string) string   { return s.paint(colorInfo, text) }
func (s *bannerStyle) dim(text string) string    { return s.paint(colorDim, text) }
func (s *bannerStyle) accent(text string) string { return s.paint(colorAccent, text) }

// isStderrTTY reports whether w is a terminal file using go-isatty.
// Colors only when TTY, plain bytes otherwise.
func isStderrTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

// renderBanner writes the unicode banner in the eka CLI visual language:
// section headers, tree glyphs └──, aligned key-value rows. It is a pure
// function of st and s.Color — deterministic for identical state.
func renderBanner(s *bannerStyle, st bannerState) {
	w := s.W
	// Top title: blank line separation then accent heading.
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, s.accent("EKA MCP"))
	fmt.Fprintln(w, s.dim("stdio MCP server — EKA AI-agent integration layer"))
	fmt.Fprintln(w, "")

	// Runtime section
	renderSection(s, "Runtime", [][2]string{
		{"eka-mcp", st.Version},
		{"pack", fmt.Sprintf("%s v%s (%s)", st.PackName, st.PackVersion, st.PackStatus)},
		{"protocol", st.Protocol},
		{"transport", st.Transport},
	})
	fmt.Fprintln(w, "")

	// Workspace section
	initStr := "yes"
	if !st.Initialized {
		initStr = "no — not initialized"
	}
	renderSection(s, "Workspace", [][2]string{
		{"path", st.WorkspacePath},
		{"initialized", initStr},
		{"projects", fmt.Sprintf("%d", st.Projects)},
		{"repositories", fmt.Sprintf("%d", st.Repos)},
	})
	if !st.Initialized {
		// Deterministic hint for uninitialized state, never a crash.
		fmt.Fprintf(w, "%s %s\n", s.dim(treeLast), s.dim("run 'eka project register' to create it"))
	}
	fmt.Fprintln(w, "")

	// Capabilities section
	renderSection(s, "Capabilities", [][2]string{
		{"tools", fmt.Sprintf("%d", st.ToolCount)},
		{"resources", fmt.Sprintf("%d", st.ResourceCount)},
	})
	fmt.Fprintln(w, "")

	// Hints section
	renderSection(s, "Hints", [][2]string{
		{"attach", "eka-mcp configure --target opencode --dir . --json   (or claude, codex)"},
		{"stop", "press Ctrl-C"},
	})
	fmt.Fprintln(w, "")
}

// renderSection prints one section header (accent) followed by aligned
// key-value rows prefixed with the tree glyph └──. Labels are Info-colored
// when colors are enabled; the glyph is Dim.
func renderSection(s *bannerStyle, title string, rows [][2]string) {
	w := s.W
	fmt.Fprintln(w, s.accent(title))
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		label := r[0]
		value := r[1]
		padded := fmt.Sprintf("%-*s", width, label)
		glyph := s.dim(treeLast)
		colored := s.info(padded)
		// Aligned key-value: "└── label···   value"
		fmt.Fprintf(w, "%s %s   %s\n", glyph, colored, value)
	}
}

// bannerColorEnabled decides whether the banner should emit colors: TTY
// plus NO_COLOR/TERM checks. The caller passes stderr so non-TTY (pipe,
// bytes.Buffer in tests) yields plain bytes.
func bannerColorEnabled(w io.Writer) bool {
	if !isStderrTTY(w) {
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

// isTTY is the stdin gate: whether the banner should be shown at all.
// The caller uses go-isatty on stdin; this helper centralizes the check
// for testability.
func shouldShowBanner(isStdinTTY bool) bool { return isStdinTTY }

// writeBannerIfTTY renders the banner to out (stderr) when stdin is a TTY.
// It is the single banner entry point used by serve(). Zero bytes to out
// when not TTY. Colors only when out is a TTY.
func writeBannerIfTTY(out io.Writer, isStdinTTY bool, cap *eka.Capability) {
	if !shouldShowBanner(isStdinTTY) {
		return
	}
	st, _ := collectBannerState(cap)
	color := bannerColorEnabled(out)
	bs := &bannerStyle{Color: color, W: out}
	renderBanner(bs, st)
}
