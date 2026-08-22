package pack

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// renderFrontmatterKeys extracts the frontmatter key lines between the ---
// delimiters of a rendered command file.
func renderFrontmatterKeys(t *testing.T, rendered []byte) []string {
	t.Helper()
	text := string(rendered)
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("rendered command must start with frontmatter, got %q", text[:min(20, len(text))])
	}
	rest := text[len("---\n"):]
	closing := strings.Index(rest, "\n---\n")
	if closing < 0 {
		t.Fatalf("rendered command has unterminated frontmatter:\n%s", text)
	}
	var keys []string
	for _, line := range strings.Split(rest[:closing], "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, _, _ := strings.Cut(line, ":")
		keys = append(keys, key)
	}
	return keys
}

// TestRenderCommandPerTarget (req R3/R5, spike V2): rendering is
// deterministic per target, preserves the description verbatim, keeps the
// body byte-identical to the canonical body, and emits ONLY keys that
// resolve — today none do for any target, so every target renders a
// description-only frontmatter (omission over invention).
func TestRenderCommandPerTarget(t *testing.T) {
	cmds, err := CommandFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) == 0 {
		t.Fatal("the pack must embed at least one command")
	}
	for _, target := range InstallTargets {
		for _, cmd := range cmds {
			t.Run(target+"/"+cmd, func(t *testing.T) {
				embedded, err := fs.ReadFile(packFS, filepath.Join("commands", cmd))
				if err != nil {
					t.Fatal(err)
				}
				wantDescription, wantBody, err := splitCommandFrontmatter(cmd, embedded)
				if err != nil {
					t.Fatal(err)
				}
				first, err := RenderCommand(target, cmd)
				if err != nil {
					t.Fatalf("RenderCommand(%q, %q): %v", target, cmd, err)
				}
				second, err := RenderCommand(target, cmd)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first, second) {
					t.Fatal("render must be deterministic (two renders byte-identical)")
				}
				gotDescription, gotBody, err := splitCommandFrontmatter(cmd, first)
				if err != nil {
					t.Fatalf("rendered output must re-parse as canonical frontmatter: %v", err)
				}
				if gotDescription != wantDescription {
					t.Errorf("description drifted:\n got = %q\nwant = %q", gotDescription, wantDescription)
				}
				if gotBody != wantBody {
					t.Errorf("body must stay byte-identical to the canonical body")
				}
				keys := renderFrontmatterKeys(t, first)
				if !reflect.DeepEqual(keys, []string{"description"}) {
					t.Errorf("frontmatter keys = %v, want exactly [description] (V2: omit unresolvable provider keys)", keys)
				}
			})
		}
	}
}

// TestRenderCommandIdentityOnCanonicalBody: canonical bodies already carry
// description-only frontmatter, so with no resolvable provider keys the
// rendered output is byte-identical to the embedded file.
func TestRenderCommandIdentityOnCanonicalBody(t *testing.T) {
	cmds, err := CommandFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range cmds {
		embedded, err := fs.ReadFile(packFS, filepath.Join("commands", cmd))
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := RenderCommand("opencode", cmd)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(embedded, rendered) {
			t.Errorf("%s: rendering a description-only canonical body must be identity, got drift", cmd)
		}
	}
}

// TestRenderCommandRefusals: unknown names and path-shaped names refuse
// deterministically (the embedded filesystem is the source of truth; no
// traversal through target/command names).
func TestRenderCommandRefusals(t *testing.T) {
	for _, name := range []string{"bogus.md", "../eka-discuss.md", "sub/eka-discuss.md", "", "."} {
		if _, err := RenderCommand("opencode", name); err == nil {
			t.Errorf("RenderCommand(%q) must refuse, got nil error", name)
		}
	}
	if _, err := RenderCommand("bogus-target", "eka-discuss.md"); err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Errorf("unknown target must refuse with supported list, got %v", err)
	}
}

// TestSidecarTextMatchesRenderText (req R5): the sidecar content is exactly
// the active mapping table's RenderText output — same table, same bytes.
func TestSidecarTextMatchesRenderText(t *testing.T) {
	ecosystems, err := MappingEcosystems()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range ecosystems {
		table, err := LoadMappingTable(key)
		if err != nil {
			t.Fatal(err)
		}
		want, err := table.RenderText()
		if err != nil {
			t.Fatal(err)
		}
		got, err := SidecarText(key)
		if err != nil {
			t.Fatalf("SidecarText(%q): %v", key, err)
		}
		if got != want {
			t.Errorf("SidecarText(%q) differs from RenderText output", key)
		}
		a, _ := SidecarText(key)
		b, _ := SidecarText(key)
		if a != b {
			t.Errorf("SidecarText(%q) must be deterministic", key)
		}
	}
	if _, err := SidecarText("bogus"); err == nil {
		t.Error("unknown ecosystem must refuse")
	}
}

// TestResolveLayout pins the conventional per-target layout (spike V3):
// opencode under .config/opencode, claude under .claude, codex skills-only
// under .agents/skills — and NO prompts directory anywhere.
func TestResolveLayout(t *testing.T) {
	base := string(filepath.Separator) + filepath.Join("tmp", "base")
	cases := []struct {
		target      string
		skills      string
		commands    string // "" = no command target
		sidecar     string
	}{
		{"opencode", filepath.Join(base, ".config", "opencode", "skills"), filepath.Join(base, ".config", "opencode", "commands"), filepath.Join(base, ".config", "opencode", "commands")},
		{"claude", filepath.Join(base, ".claude", "skills"), filepath.Join(base, ".claude", "commands"), filepath.Join(base, ".claude", "commands")},
		{"codex", filepath.Join(base, ".agents", "skills"), "", filepath.Join(base, ".agents", "skills")},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			layout, err := ResolveLayout(tc.target, base)
			if err != nil {
				t.Fatal(err)
			}
			if layout.SkillsDir != tc.skills {
				t.Errorf("SkillsDir = %q, want %q", layout.SkillsDir, tc.skills)
			}
			if layout.CommandsDir != tc.commands {
				t.Errorf("CommandsDir = %q, want %q", layout.CommandsDir, tc.commands)
			}
			if layout.SidecarDir != tc.sidecar {
				t.Errorf("SidecarDir = %q, want %q", layout.SidecarDir, tc.sidecar)
			}
			if strings.Contains(layout.CommandsDir+layout.SkillsDir, "prompts") {
				t.Errorf("codex prompts dir is dead (0.117.0); layout must not reference it: %+v", layout)
			}
		})
	}
	if _, err := ResolveLayout("bogus", base); err == nil || !strings.Contains(err.Error(), "supported targets") {
		t.Errorf("unknown target must refuse deterministically, got %v", err)
	}
}

// TestInstallForTargetDryRunWritesNothing (req R7): dry-run reports the full
// plan (paths + create actions) but creates nothing — not even directories.
func TestInstallForTargetDryRunWritesNothing(t *testing.T) {
	base := t.TempDir()
	for _, target := range InstallTargets {
		t.Run(target, func(t *testing.T) {
			root := filepath.Join(base, target)
			// codex has no command target (spike V3) — plan skills only.
			withCommands := target != "codex"
			rep, err := InstallForTarget(target, root, true, withCommands, true)
			if err != nil {
				t.Fatalf("dry-run plan: %v", err)
			}
			if len(rep.Files["skills"]) == 0 {
				t.Fatalf("plan must cover skills, got %v", rep.Files)
			}
			if withCommands && len(rep.Files["commands"]) == 0 {
				t.Fatalf("plan must cover commands, got %v", rep.Files)
			}
			if len(rep.Actions) == 0 {
				t.Fatal("dry-run must report actions")
			}
			for _, a := range rep.Actions {
				if a.Action != "create" {
					t.Errorf("fresh dry-run action = %q, want create (%s)", a.Action, a.Path)
				}
				if !strings.HasPrefix(a.Path, root) {
					t.Errorf("action path %q escapes the install base %q", a.Path, root)
				}
			}
			if rep.Counts.Created != len(rep.Actions) || rep.Counts.Overwritten != 0 || rep.Counts.Skipped != 0 {
				t.Errorf("counts = %+v, want all-create over %d actions", rep.Counts, len(rep.Actions))
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Errorf("dry-run must not create anything, stat err = %v", err)
			}
			// The sidecar must be part of the plan at the documented location.
			layout, _ := ResolveLayout(target, root)
			sidecarPath := filepath.Join(layout.SidecarDir, SidecarName)
			found := false
			for _, a := range rep.Actions {
				if a.Path == sidecarPath {
					found = true
				}
			}
			if !found {
				t.Errorf("sidecar %s missing from plan: %v", sidecarPath, rep.Actions)
			}
		})
	}
}

// TestInstallForTargetCodexSubtreeLayout (spike V3): codex installs ONLY the
// skills subtree under .agents/skills with DELEGATION.txt INSIDE it; no
// commands land anywhere and no prompts dir is referenced.
func TestInstallForTargetCodexSubtreeLayout(t *testing.T) {
	base := t.TempDir()
	rep, err := InstallForTarget("codex", base, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	dirs, _ := SkillDirs()
	if !equalStrings(rep.Files["skills"], dirs) {
		t.Errorf("installed skills = %v, want %v", rep.Files["skills"], dirs)
	}
	if _, ok := rep.Files["commands"]; ok {
		t.Errorf("codex install must not report commands, got %v", rep.Files)
	}
	skillsRoot := filepath.Join(base, ".agents", "skills")
	for _, s := range dirs {
		if _, err := os.Stat(filepath.Join(skillsRoot, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s must install under .agents/skills: %v", s, err)
		}
	}
	sidecar, err := os.ReadFile(filepath.Join(skillsRoot, SidecarName))
	if err != nil {
		t.Fatalf("DELEGATION.txt must sit inside the skills subtree: %v", err)
	}
	want, _ := SidecarText("codex")
	if string(sidecar) != want {
		t.Errorf("codex sidecar content drifted from RenderText output")
	}
	if strings.Contains(string(sidecar), "<!--") || strings.HasSuffix(strings.ToLower(SidecarName), ".md") {
		t.Errorf("sidecar must never be a .md file (V1)")
	}
}

// TestInstallForTargetCodexRefusesCommands (req R7): codex has no command
// target since codex-cli 0.117.0 removed ~/.codex/prompts — the refusal is
// deterministic and writes nothing.
func TestInstallForTargetCodexRefusesCommands(t *testing.T) {
	base := t.TempDir()
	_, err := InstallForTarget("codex", base, true, true, false)
	if err == nil || !strings.Contains(err.Error(), "no command directory") {
		t.Fatalf("codex --with-commands must refuse, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, ".agents")); !os.IsNotExist(err) {
		t.Errorf("refused run must not write anything, stat err = %v", err)
	}
}

// TestInstallForTargetUnsupportedRefused (req R7): unknown targets refuse
// deterministically naming the supported set.
func TestInstallForTargetUnsupportedRefused(t *testing.T) {
	_, err := InstallForTarget("cursor", t.TempDir(), true, true, false)
	if err == nil || !strings.Contains(err.Error(), `unsupported target "cursor"`) ||
		!strings.Contains(err.Error(), "opencode, claude, codex") {
		t.Fatalf("unsupported target refusal drifted, got %v", err)
	}
}

// TestInstallForTargetIdempotentScoping (req R5 reinstall semantics): the
// second run overwrites only pack-owned files, skips identical content,
// leaves foreign files untouched, and reports stable counts.
func TestInstallForTargetIdempotentScoping(t *testing.T) {
	base := t.TempDir()
	first, err := InstallForTarget("opencode", base, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Counts.Created == 0 || first.Counts.Overwritten != 0 {
		t.Fatalf("first run counts = %+v, want creates only", first.Counts)
	}
	layout, _ := ResolveLayout("opencode", base)

	// Foreign files in both install dirs must survive reinstalls untouched.
	foreignSkill := filepath.Join(layout.SkillsDir, "foreign-skill", "notes.txt")
	foreignCmd := filepath.Join(layout.CommandsDir, "my-own-command.md")
	if err := os.MkdirAll(filepath.Dir(foreignSkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignSkill, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignCmd, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, err := InstallForTarget("opencode", base, true, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Counts.Created != 0 {
		t.Errorf("second run created %d files, want 0 (idempotent)", second.Counts.Created)
	}
	if second.Counts.Skipped != len(second.Actions) {
		t.Errorf("second run counts = %+v over %d actions, want all skip (content unchanged)", second.Counts, len(second.Actions))
	}
	gotForeign, err := os.ReadFile(foreignCmd)
	if err != nil || string(gotForeign) != "# mine\n" {
		t.Errorf("foreign command file must survive untouched: %v %q", err, gotForeign)
	}
	gotForeignSkill, err := os.ReadFile(foreignSkill)
	if err != nil || string(gotForeignSkill) != "mine\n" {
		t.Errorf("foreign skill file must survive untouched: %v %q", err, gotForeignSkill)
	}

	// Mutating a pack-owned file makes exactly that file an overwrite again.
	cmdPath := filepath.Join(layout.CommandsDir, "eka-discuss.md")
	if err := os.WriteFile(cmdPath, []byte("---\ndescription: tampered\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := InstallForTarget("opencode", base, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if third.Counts.Overwritten != 1 {
		t.Errorf("tampered reinstall overwrites = %d, want exactly 1", third.Counts.Overwritten)
	}
	rendered, _ := RenderCommand("opencode", "eka-discuss.md")
	got, _ := os.ReadFile(cmdPath)
	if !bytes.Equal(got, rendered) {
		t.Error("overwritten command must byte-match the rendered pack content")
	}
}

// TestInstallForTargetSymlinkRefused: a symlink sitting on a pack-owned
// path refuses the whole install — nothing is written through the link and
// the link itself stays intact. (A hard link is indistinguishable from a
// regular file by design; the rename-based writer never touches its inode.)
func TestInstallForTargetSymlinkRefused(t *testing.T) {
	base := t.TempDir()
	layout, _ := ResolveLayout("claude", base)
	if err := os.MkdirAll(layout.CommandsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(base, "victim.txt")
	if err := os.WriteFile(victim, []byte("do not touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(layout.CommandsDir, "eka-discuss.md")
	if err := os.Symlink(victim, linkPath); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	_, err := InstallForTarget("claude", base, false, true, false)
	if err == nil || !strings.Contains(err.Error(), "not a regular pack-owned file") {
		t.Fatalf("symlinked command target must refuse, got %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != "do not touch\n" {
		t.Errorf("link target must stay untouched: %v %q", err, got)
	}
	info, err := os.Lstat(linkPath)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink itself must remain a symlink, got %+v (%v)", info, err)
	}
}

// TestInstallBaseResolution: explicit dirs win and are absolutized (with ~
// expansion); opencode/claude fall back to HOME, codex to the working dir.
func TestInstallBaseResolution(t *testing.T) {
	tmp := t.TempDir()
	got, err := InstallBase("opencode", tmp)
	if err != nil || got != tmp {
		t.Errorf("explicit dir must win: %q vs %q (%v)", got, tmp, err)
	}
	tilde := "~/eka-test-anchor"
	got, err = InstallBase("claude", tilde)
	home, _ := os.UserHomeDir()
	if err != nil || got != filepath.Join(home, "eka-test-anchor") {
		t.Errorf("~ must expand safely: %q (%v)", got, err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	got, err = InstallBase("claude", "")
	if err != nil || got != tmp {
		t.Errorf("claude without dir must anchor at home %q, got %q (%v)", tmp, got, err)
	}
	wd, _ := os.Getwd()
	got, err = InstallBase("codex", "")
	if err != nil || got != wd {
		t.Errorf("codex without dir must anchor at cwd %q, got %q (%v)", wd, got, err)
	}
	if _, err := InstallBase("bogus", ""); err == nil {
		t.Error("unknown target must refuse")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
