package main

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testTheme() *theme { return &theme{Color: false} }

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "q":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func update(m installModel, msg tea.Msg) installModel {
	next, _ := m.Update(msg)
	return next.(installModel)
}

// TestInstallFlowKindToResources: enter on kind loads the checklist
// with every non-disabled entry selected by default.
func TestInstallFlowKindToResources(t *testing.T) {
	m := newInstallModel(&bytes.Buffer{})
	m.th = testTheme()
	m = update(m, keyMsg("enter")) // skills
	if m.step != stepResources {
		t.Fatalf("step = %v, want resources", m.step)
	}
	if m.kind != "skills" {
		t.Fatalf("kind = %q, want skills", m.kind)
	}
	if len(m.items) == 0 {
		t.Fatal("checklist must not be empty")
	}
	for _, it := range m.items {
		if !it.disabled && !it.selected {
			t.Errorf("enabled item %q must default selected", it.name)
		}
		if it.disabled && it.selected {
			t.Errorf("disabled item %q must default unselected", it.name)
		}
	}
}

// TestInstallToggleSkipsDisabled: space toggles enabled rows only;
// disabled rows never change.
func TestInstallToggleSkipsDisabled(t *testing.T) {
	m := newInstallModel(&bytes.Buffer{})
	m.th = testTheme()
	m = update(m, keyMsg("enter"))
	if len(m.items) == 0 {
		t.Skip("no skills in pack")
	}
	// Toggle the cursor row twice: net effect must be identity.
	before := m.items[m.cursor].selected
	m = update(m, keyMsg(" "))
	m = update(m, keyMsg(" "))
	if m.items[m.cursor].disabled {
		t.Skip("cursor on disabled row in this environment")
	}
	if m.items[m.cursor].selected != before {
		t.Errorf("double toggle must be identity")
	}
	// Force cursor onto a disabled row if one exists: toggle is a no-op.
	for i := range m.items {
		if m.items[i].disabled {
			m.cursor = i
			m = update(m, keyMsg(" "))
			if m.items[i].selected {
				t.Errorf("disabled row %q became selected", m.items[i].name)
			}
		}
	}
}

// TestInstallEmptySelectionBlocked: enter with nothing selected stays
// on the step with a hint.
func TestInstallEmptySelectionBlocked(t *testing.T) {
	m := newInstallModel(&bytes.Buffer{})
	m.th = testTheme()
	m = update(m, keyMsg("enter"))
	for i := range m.items {
		m.items[i].selected = false
	}
	m = update(m, keyMsg("enter"))
	if m.step != stepResources {
		t.Errorf("empty selection must stay on resources, at %v", m.step)
	}
	if m.hint == "" {
		t.Error("empty selection must set a hint")
	}
}

// TestInstallCodexDisabledForCommands: codex cannot be selected when
// kind is commands.
func TestInstallCodexDisabledForCommands(t *testing.T) {
	m := newInstallModel(&bytes.Buffer{})
	m.th = testTheme()
	m = update(m, keyMsg("down")) // commands
	m = update(m, keyMsg("enter"))
	m = update(m, keyMsg("enter")) // agents (all resources default selected)
	if m.step != stepAgents {
		t.Fatalf("step = %v, want agents", m.step)
	}
	if !m.agentDis["codex"] {
		t.Error("codex must be disabled for commands")
	}
	if m.agentSel["codex"] {
		t.Error("codex must default unselected for commands")
	}
	// Toggle attempt on the codex row is a no-op.
	for i, a := range m.agents {
		if a == "codex" {
			m.agentCur = i
		}
	}
	m = update(m, keyMsg(" "))
	if m.agentSel["codex"] {
		t.Error("disabled codex became selected")
	}
}

// TestInstallFullFlowToConfirm: skills → agents → scope lands on the
// confirm screen with per-agent dry-run plans.
func TestInstallFullFlowToConfirm(t *testing.T) {
	m := newInstallModel(&bytes.Buffer{})
	m.th = testTheme()
	m = update(m, keyMsg("enter")) // skills
	m = update(m, keyMsg("enter")) // agents
	m = update(m, keyMsg("enter")) // scope (global)
	m = update(m, keyMsg("enter")) // confirm
	if m.step != stepConfirm {
		t.Fatalf("step = %v, want confirm (hint %q)", m.step, m.hint)
	}
	if len(m.plans) == 0 {
		t.Error("confirm must carry dry-run plans")
	}
	for a, rep := range m.plans {
		if len(rep.Actions) == 0 {
			t.Errorf("plan for %s must not be empty", a)
		}
	}
	// Back navigation returns to scope.
	m = update(m, keyMsg("esc"))
	if m.step != stepScope {
		t.Errorf("esc must go back to scope, at %v", m.step)
	}
}

// TestInstallViewThemed: views carry the global margin, glyphs and
// step titles on every step.
func TestInstallViewThemed(t *testing.T) {
	m := newInstallModel(&bytes.Buffer{})
	m.th = testTheme()
	v := m.View()
	if !strings.Contains(v, themeMargin+"EKA Install") {
		t.Errorf("title must carry the global margin:\n%s", v)
	}
	if !strings.Contains(v, glyphSingleOn) && !strings.Contains(v, glyphSingleOff) {
		t.Errorf("kind step must show single-select glyphs:\n%s", v)
	}
	m = update(m, keyMsg("enter"))
	v = m.View()
	if !strings.Contains(v, "Step 2 of 4") || !strings.Contains(v, glyphMultiOn) {
		t.Errorf("resources step must show multi glyphs:\n%s", v)
	}
	// No line (except blanks) may stick to the left edge.
	for _, ln := range strings.Split(v, "\n") {
		if ln != "" && !strings.HasPrefix(ln, themeMargin) {
			t.Errorf("line sticks to the left edge: %q", ln)
		}
	}
}

// TestInstallInteractiveRefusesPiped: off-TTY runs refuse with the
// inline usage instead of opening the TUI.
func TestInstallInteractiveRefusesPiped(t *testing.T) {
	// go test never runs on a TTY stdin, so this must refuse.
	err := runInstallInteractive(&bytes.Buffer{})
	if err == nil {
		t.Fatal("piped run must refuse interactive mode")
	}
	if !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("refusal must mention the terminal, got %v", err)
	}
}

// TestInstallNoArgsPipedRefuses: `install` bare off-TTY routes to the
// refusal (not the TUI).
func TestInstallNoArgsPipedRefuses(t *testing.T) {
	if err := runInstall(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("bare install off-TTY must refuse")
	}
}
