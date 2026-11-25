package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	pack "github.com/maleolabs/eka-mcp"
)

// This file implements the interactive installer: the default when
// `eka-mcp install` runs with no arguments on a TTY. It is a modern
// multi-step flow (bubbletea) mirroring agent-CLI installers:
//
//	step 1 — kind: single-select skills/commands;
//	step 2 — resources: multi-select checklist (space toggles, default
//	  all selected, entries already installed everywhere disabled);
//	step 3 — agents: multi-select opencode/claude/codex
//	  (codex disabled when kind is commands — no command target);
//	step 4 — scope: single-select global/repo;
//	step 5 — confirm: per-target dry-run counts, enter executes.
//
// Styling follows the shared theme (theme.go): 2-space global margin,
// blank line between sections, ◉/○ single glyphs, ■/□ multi glyphs,
// › cursor, dimmed disabled rows. The model is pure (Update/View need
// no TTY) so every step is unit-testable; the tea.Program only runs on
// a real terminal.

type installStep int

const (
	stepKind installStep = iota
	stepResources
	stepAgents
	stepScope
	stepConfirm
	stepDone
)

// resourceItem is one checklist row of step 2.
type resourceItem struct {
	name        string
	installedIn []string // "agent·scope" combos already holding it
	disabled    bool     // installed in every applicable combo
	selected    bool
}

// installModel is the interactive installer state.
type installModel struct {
	th      *theme
	step    installStep
	kinds   []string
	kindCur int
	kind    string

	items  []resourceItem
	cursor int

	agents     []string
	agentCur   int
	agentSel   map[string]bool
	agentDis   map[string]bool // reason-tagged disables
	agentNote  map[string]string
	scopes     []string
	scopeCur   int
	scope      string
	scopeDescs map[string]string

	plans   map[string]pack.TargetInstallReport
	reports map[string]pack.TargetInstallReport
	execErr error
	aborted bool
	hint    string
}

var installScopes = []string{"global", "repo"}

var installScopeDescs = map[string]string{
	"global": "user home (opencode, claude) or working dir (codex)",
	"repo":   "current directory anchors the tree (project-scoped)",
}

// runInstallInteractive is the no-argument `install` entry: it refuses
// off-TTY (piped runs get the inline usage instead) and otherwise runs
// the bubbletea program, then prints the themed outcome.
func runInstallInteractive(out io.Writer) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("install: interactive mode needs a terminal; use the inline form instead: install <skills|commands> --dir <dir> [--dry-run] --json")
	}
	m := newInstallModel(out)
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return fmt.Errorf("install: interactive session failed: %w", err)
	}
	fm, ok := final.(installModel)
	if !ok {
		return fmt.Errorf("install: interactive session failed: unexpected model")
	}
	if fm.aborted {
		t := newTheme(out)
		fmt.Fprintln(t.W, t.dim("cancelled — nothing installed."))
		return nil
	}
	return fm.execErr
}

func newInstallModel(out io.Writer) installModel {
	return installModel{
		th:        &theme{Color: themeColorEnabled(out)},
		kinds:     []string{"skills", "commands"},
		agents:    append([]string(nil), pack.InstallTargets...),
		agentSel:  map[string]bool{},
		agentDis:  map[string]bool{},
		agentNote: map[string]string{},
		scopes:    append([]string(nil), installScopes...),
		scopeDescs: map[string]string{
			"global": installScopeDescs["global"],
			"repo":   installScopeDescs["repo"],
		},
		plans:   map[string]pack.TargetInstallReport{},
		reports: map[string]pack.TargetInstallReport{},
	}
}

// scopeDir maps the chosen scope to the --dir anchor: global uses the
// conventional base ("" → InstallBase default), repo anchors at cwd.
func (m *installModel) scopeDir() string {
	if m.scope == "repo" {
		if cwd, err := os.Getwd(); err == nil {
			return cwd
		}
	}
	return ""
}

func (m installModel) Init() tea.Cmd { return nil }

// selectedAgents returns the chosen agents in InstallTargets order.
func (m *installModel) selectedAgents() []string {
	var out []string
	for _, a := range m.agents {
		if m.agentSel[a] {
			out = append(out, a)
		}
	}
	return out
}

// selectedNames returns the chosen resource names in list order.
func (m *installModel) selectedNames() []string {
	var out []string
	for _, it := range m.items {
		if it.selected && !it.disabled {
			out = append(out, it.name)
		}
	}
	return out
}

// loadResources builds the step-2 checklist for the chosen kind:
// every known entry, default all selected, entries already installed
// in every applicable destination disabled with their locations
// tagged. Applicable destinations are agents × {global, repo}; codex
// has no command destination, so commands probe opencode/claude only.
func (m *installModel) loadResources() {
	var names []string
	if m.kind == "skills" {
		names, _ = pack.SkillDirs()
	} else {
		names, _ = pack.CommandFiles()
	}
	m.items = m.items[:0]
	m.cursor = 0
	for _, n := range names {
		in := probeInstalled(m.kind, n)
		need := 6
		if m.kind == "commands" {
			need = 4 // opencode/claude × global/repo; codex has no command target
		}
		dis := len(in) >= need
		m.items = append(m.items, resourceItem{name: n, installedIn: in, disabled: dis, selected: !dis})
	}
}

// probeInstalled reports the "agent·scope" combos already holding one
// resource: skill dirs by existence, command files by existence.
func probeInstalled(kind, name string) []string {
	var in []string
	cwd, _ := os.Getwd()
	for _, agent := range pack.InstallTargets {
		if kind == "commands" && agent == "codex" {
			continue
		}
		for _, sc := range []struct {
			tag string
			dir string
		}{{"global", ""}, {"repo", cwd}} {
			base, err := pack.InstallBase(agent, sc.dir)
			if err != nil {
				continue
			}
			layout, err := pack.ResolveLayout(agent, base)
			if err != nil {
				continue
			}
			switch kind {
			case "skills":
				if fi, err := os.Stat(filepath.Join(layout.SkillsDir, name)); err == nil && fi.IsDir() {
					in = append(in, agent+"·"+sc.tag)
				}
			case "commands":
				if layout.CommandsDir == "" {
					continue
				}
				if fi, err := os.Stat(filepath.Join(layout.CommandsDir, name)); err == nil && fi.Mode().IsRegular() {
					in = append(in, agent+"·"+sc.tag)
				}
			}
		}
	}
	sort.Strings(in)
	return in
}

// enterAgents preselects agents for step 3: all enabled by default;
// codex is disabled with a reason tag when kind is commands.
func (m *installModel) enterAgents() {
	m.agentCur = 0
	for _, a := range m.agents {
		delete(m.agentDis, a)
		delete(m.agentNote, a)
		if m.kind == "commands" && a == "codex" {
			m.agentDis[a] = true
			m.agentNote[a] = "no command target"
			m.agentSel[a] = false
		} else if _, ok := m.agentSel[a]; !ok {
			m.agentSel[a] = true
		}
	}
}

// planAll dry-runs the current selection per agent for the confirm
// screen; a planning error aborts into the hint line.
func (m *installModel) planAll() {
	m.plans = map[string]pack.TargetInstallReport{}
	m.hint = ""
	names := m.selectedNames()
	for _, a := range m.selectedAgents() {
		var rep pack.TargetInstallReport
		var err error
		if m.kind == "skills" {
			rep, err = pack.InstallSelection(a, m.scopeDir(), names, nil, true)
		} else {
			rep, err = pack.InstallSelection(a, m.scopeDir(), nil, names, true)
		}
		if err != nil {
			m.hint = err.Error()
			return
		}
		m.plans[a] = rep
	}
}

// execute runs the confirmed selection per agent (real writes).
func (m *installModel) execute() {
	m.reports = map[string]pack.TargetInstallReport{}
	m.execErr = nil
	names := m.selectedNames()
	for _, a := range m.selectedAgents() {
		var rep pack.TargetInstallReport
		var err error
		if m.kind == "skills" {
			rep, err = pack.InstallSelection(a, m.scopeDir(), names, nil, false)
		} else {
			rep, err = pack.InstallSelection(a, m.scopeDir(), nil, names, false)
		}
		if err != nil {
			m.execErr = err
			return
		}
		m.reports[a] = rep
	}
	m.step = stepDone
}

func (m installModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.aborted = true
			return m, tea.Quit
		case "esc", "backspace":
			m.hint = ""
			if m.step > stepKind && m.step < stepDone {
				m.step--
			}
			return m, nil
		case "up", "k":
			m.moveCursor(-1)
			return m, nil
		case "down", "j":
			m.moveCursor(1)
			return m, nil
		case " ":
			m.toggle()
			return m, nil
		case "enter":
			m.confirm()
			if m.step == stepDone && !m.aborted {
				return m, tea.Quit
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *installModel) moveCursor(d int) {
	switch m.step {
	case stepKind:
		m.kindCur = (m.kindCur + d + len(m.kinds)) % len(m.kinds)
	case stepResources:
		if len(m.items) == 0 {
			return
		}
		m.cursor = (m.cursor + d + len(m.items)) % len(m.items)
	case stepAgents:
		m.agentCur = (m.agentCur + d + len(m.agents)) % len(m.agents)
	case stepScope:
		m.scopeCur = (m.scopeCur + d + len(m.scopes)) % len(m.scopes)
	}
}

func (m *installModel) toggle() {
	switch m.step {
	case stepResources:
		if len(m.items) == 0 {
			return
		}
		it := &m.items[m.cursor]
		if it.disabled {
			return
		}
		it.selected = !it.selected
	case stepAgents:
		a := m.agents[m.agentCur]
		if m.agentDis[a] {
			return
		}
		m.agentSel[a] = !m.agentSel[a]
	}
}

func (m *installModel) confirm() {
	m.hint = ""
	switch m.step {
	case stepKind:
		m.kind = m.kinds[m.kindCur]
		m.loadResources()
		m.step = stepResources
	case stepResources:
		if len(m.selectedNames()) == 0 {
			m.hint = "select at least one resource (space toggles)"
			return
		}
		m.enterAgents()
		m.step = stepAgents
	case stepAgents:
		if len(m.selectedAgents()) == 0 {
			m.hint = "select at least one agent (space toggles)"
			return
		}
		m.step = stepScope
	case stepScope:
		m.scope = m.scopes[m.scopeCur]
		m.planAll()
		if m.hint == "" {
			m.step = stepConfirm
		}
	case stepConfirm:
		m.execute()
	}
}

// View renders the current step: title, blank-separated sections,
// 2-space global margin, glyphs ◉/○ (single) ■/□ (multi) › (cursor).
func (m installModel) View() string {
	var b strings.Builder
	t := m.th
	margined := func(s string) {
		for _, ln := range strings.Split(s, "\n") {
			if ln == "" {
				b.WriteString("\n")
			} else {
				b.WriteString(themeMargin + ln + "\n")
			}
		}
	}
	line := func(s string) { margined(s) }
	blank := func() { b.WriteString("\n") }

	line(t.accent("EKA Install"))
	blank()

	switch m.step {
	case stepKind:
		line(t.dim("Step 1 of 4 — what to install (single choice)"))
		blank()
		for i, k := range m.kinds {
			g := glyphSingleOff
			if i == m.kindCur {
				g = glyphSingleOn
			}
			row := g + "  " + k
			if i == m.kindCur {
				row = t.info(glyphCursor + " " + g + "  " + k)
			} else {
				row = "  " + g + "  " + k
			}
			line(row)
		}
		blank()
		line(t.dim("↑/↓ move · enter select · q quit"))
	case stepResources:
		line(t.dim("Step 2 of 4 — resources to install (space toggles, all selected by default)"))
		blank()
		if len(m.items) == 0 {
			line(t.warning("no " + m.kind + " found in the pack"))
		}
		for i, it := range m.items {
			g := glyphMultiOff
			if it.selected {
				g = glyphMultiOn
			}
			row := g + "  " + it.name
			switch {
			case it.disabled:
				row = t.dim("  " + g + "  " + it.name + "  (installed everywhere)")
			case i == m.cursor:
				row = t.info(glyphCursor+" "+g+"  "+it.name) + dimTag(t, it)
			default:
				row = "  " + row + dimTag(t, it)
			}
			line(row)
		}
		blank()
		line(t.dim("↑/↓ move · space toggle · enter continue · esc back · q quit"))
	case stepAgents:
		line(t.dim("Step 3 of 4 — agents to install to (multi choice)"))
		blank()
		for i, a := range m.agents {
			g := glyphMultiOff
			if m.agentSel[a] {
				g = glyphMultiOn
			}
			switch {
			case m.agentDis[a]:
				line(t.dim("  " + g + "  " + a + "  (" + m.agentNote[a] + ")"))
			case i == m.agentCur:
				line(t.info(glyphCursor + " " + g + "  " + a))
			default:
				line("  " + g + "  " + a)
			}
		}
		blank()
		line(t.dim("↑/↓ move · space toggle · enter continue · esc back · q quit"))
	case stepScope:
		line(t.dim("Step 4 of 4 — install scope (single choice)"))
		blank()
		for i, s := range m.scopes {
			g := glyphSingleOff
			if i == m.scopeCur {
				g = glyphSingleOn
			}
			if i == m.scopeCur {
				line(t.info(glyphCursor+" "+g+"  "+s) + t.dim("  — "+m.scopeDescs[s]))
			} else {
				line("  " + g + "  " + s + t.dim("  — "+m.scopeDescs[s]))
			}
		}
		blank()
		line(t.dim("↑/↓ move · enter continue · esc back · q quit"))
	case stepConfirm:
		line(t.dim("Confirm — dry-run plan (enter installs, esc goes back)"))
		blank()
		names := m.selectedNames()
		line(t.accent("Selection"))
		for _, r := range [][2]string{
			{"kind", m.kind},
			{"resources", fmt.Sprintf("%d", len(names))},
			{"agents", strings.Join(m.selectedAgents(), ", ")},
			{"scope", m.scope},
		} {
			line("  " + t.dim(themeTreeLast) + " " + t.info(r[0]) + "   " + r[1])
		}
		blank()
		for _, a := range m.selectedAgents() {
			rep := m.plans[a]
			line(t.accent(a))
			line("  " + t.dim(themeTreeLast) + " " + fmt.Sprintf("%d create, %d overwrite, %d skip", rep.Counts.Created, rep.Counts.Overwritten, rep.Counts.Skipped))
		}
		blank()
		line(t.dim("enter install · esc back · q quit"))
	case stepDone:
		if m.execErr != nil {
			line(t.failure("install failed: " + m.execErr.Error()))
		} else {
			line(t.success("installed"))
			blank()
			for _, a := range m.selectedAgents() {
				rep := m.reports[a]
				line(t.accent(a))
				line("  " + t.dim(themeTreeLast) + " " + fmt.Sprintf("%d create, %d overwrite, %d skip", rep.Counts.Created, rep.Counts.Overwritten, rep.Counts.Skipped))
			}
		}
		blank()
		line(t.dim("enter/q quit"))
	}
	if m.hint != "" {
		blank()
		line(t.warning(m.hint))
	}
	return b.String()
}

// dimTag renders the installed-location tag of a resource row.
func dimTag(t *theme, it resourceItem) string {
	if len(it.installedIn) == 0 {
		return ""
	}
	if it.disabled {
		return ""
	}
	return t.dim("  (installed: " + strings.Join(it.installedIn, ", ") + ")")
}
