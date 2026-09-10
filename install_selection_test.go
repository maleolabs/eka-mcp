package pack

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInstallSelectionSubset: explicit subsets install only the named
// entries (dry-run), with the sidecar included whenever any family is
// non-empty.
func TestInstallSelectionSubset(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "opencode")
	skills, err := SkillDirs()
	if err != nil || len(skills) < 2 {
		t.Fatalf("need ≥2 skills, got %v, %v", skills, err)
	}
	cmds, err := CommandFiles()
	if err != nil || len(cmds) < 1 {
		t.Fatalf("need ≥1 command, got %v, %v", cmds, err)
	}
	rep, err := InstallSelection("opencode", root, skills[:1], cmds[:1], true)
	if err != nil {
		t.Fatalf("InstallSelection: %v", err)
	}
	if len(rep.Files["skills"]) != 1 || rep.Files["skills"][0] != skills[0] {
		t.Errorf("skills = %v, want [%s]", rep.Files["skills"], skills[0])
	}
	if len(rep.Files["commands"]) != 1 || rep.Files["commands"][0] != cmds[0] {
		t.Errorf("commands = %v, want [%s]", rep.Files["commands"], cmds[0])
	}
	for _, a := range rep.Actions {
		if a.Action != "create" {
			t.Errorf("fresh action = %q, want create (%s)", a.Action, a.Path)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create anything, stat err = %v", err)
	}
}

// TestInstallSelectionParity: full explicit lists match InstallForTarget.
func TestInstallSelectionParity(t *testing.T) {
	base := t.TempDir()
	a, err := InstallForTarget("claude", base, true, true, true)
	if err != nil {
		t.Fatalf("InstallForTarget: %v", err)
	}
	skills, _ := SkillDirs()
	cmds, _ := CommandFiles()
	b, err := InstallSelection("claude", base, skills, cmds, true)
	if err != nil {
		t.Fatalf("InstallSelection: %v", err)
	}
	if len(a.Actions) != len(b.Actions) {
		t.Fatalf("actions %d != %d", len(a.Actions), len(b.Actions))
	}
	for i := range a.Actions {
		if a.Actions[i] != b.Actions[i] {
			t.Fatalf("action %d differs: %+v vs %+v", i, a.Actions[i], b.Actions[i])
		}
	}
}

// TestInstallSelectionRefusals: unknown names and codex+commands refuse.
func TestInstallSelectionRefusals(t *testing.T) {
	base := t.TempDir()
	if _, err := InstallSelection("opencode", base, []string{"no-such-skill"}, nil, true); err == nil {
		t.Error("unknown skill must refuse")
	}
	if _, err := InstallSelection("opencode", base, nil, []string{"no-such-cmd.md"}, true); err == nil {
		t.Error("unknown command must refuse")
	}
	if _, err := InstallSelection("codex", base, nil, []string{"x.md"}, true); err == nil {
		t.Error("codex+commands must refuse")
	}
	if _, err := InstallSelection("nope", base, nil, nil, true); err == nil {
		t.Error("unknown target must refuse")
	}
}

// TestInstallSelectionEmpty: empty lists install nothing (no sidecar).
func TestInstallSelectionEmpty(t *testing.T) {
	base := t.TempDir()
	rep, err := InstallSelection("opencode", base, nil, nil, true)
	if err != nil {
		t.Fatalf("InstallSelection: %v", err)
	}
	if len(rep.Actions) != 0 {
		t.Errorf("empty selection must plan no actions, got %v", rep.Actions)
	}
	if len(rep.Files) != 0 {
		t.Errorf("empty selection must report no files, got %v", rep.Files)
	}
}

// TestInstallSelectionWritesSubset: real run writes only named entries.
func TestInstallSelectionWritesSubset(t *testing.T) {
	base := t.TempDir()
	skills, _ := SkillDirs()
	rep, err := InstallSelection("opencode", base, skills[:1], nil, false)
	if err != nil {
		t.Fatalf("InstallSelection: %v", err)
	}
	layout, _ := ResolveLayout("opencode", base)
	if _, err := os.Stat(filepath.Join(layout.SkillsDir, skills[0])); err != nil {
		t.Errorf("selected skill missing: %v", err)
	}
	if len(skills) > 1 {
		if _, err := os.Stat(filepath.Join(layout.SkillsDir, skills[1])); !os.IsNotExist(err) {
			t.Errorf("unselected skill must not be written: %v", err)
		}
	}
	if rep.Counts.Created == 0 {
		t.Errorf("counts must show creates: %+v", rep.Counts)
	}
}
