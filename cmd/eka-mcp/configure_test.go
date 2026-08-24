package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maleolabs/eka-mcp"
)

// helper: decode configureResult from runConfigure output.
func decodeConfigureResult(t *testing.T, out bytes.Buffer) configureResult {
	t.Helper()
	var res configureResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("configure --json must emit valid JSON: %v\n%s", err, out.String())
	}
	return res
}

func TestConfigureDefaultNoInstall(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--json"}, &out); err != nil {
		t.Fatalf("configure default failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	if res.Target != "opencode" {
		t.Errorf("target = %q, want opencode", res.Target)
	}
	if res.DryRun {
		t.Error("dryRun must be false for non-dry-run")
	}
	// Installed must be absent/empty by default (no --with-*)
	if len(res.Installed) != 0 {
		t.Errorf("default configure Installed = %v, want empty/absent", res.Installed)
	}
	// Raw JSON must not contain "installed" when empty (omitempty)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("raw json decode: %v", err)
	}
	if _, ok := raw["installed"]; ok {
		t.Errorf("raw JSON must not contain \"installed\" by default, got %s", raw["installed"])
	}
	// opencode.json must exist and contain mcp.eka entry
	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("opencode.json not written: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("opencode.json invalid json: %v", err)
	}
	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("opencode.json missing mcp: %v", cfg)
	}
	if _, ok := mcp["eka"]; !ok {
		t.Errorf("opencode.json mcp must contain eka entry, got %v", mcp)
	}
	// No skills or commands should have been copied to dir
	skills, _ := pack.SkillDirs()
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(dir, s)); !os.IsNotExist(err) {
			t.Errorf("skill %s must NOT be installed by default, stat err = %v", s, err)
		}
	}
	cmds, _ := pack.CommandFiles()
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(dir, c)); !os.IsNotExist(err) {
			t.Errorf("command %s must NOT be installed by default, stat err = %v", c, err)
		}
	}
}

func TestConfigureWithSkillsOnly(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--with-skills", "--json"}, &out); err != nil {
		t.Fatalf("configure --with-skills failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	skills, _ := pack.SkillDirs()
	if !equalStrings(res.Installed["skills"], skills) {
		t.Errorf("Installed[skills] = %v, want %v", res.Installed["skills"], skills)
	}
	if _, ok := res.Installed["commands"]; ok {
		t.Errorf("Installed must not contain commands when only --with-skills, got %v", res.Installed)
	}
	skillsDir := filepath.Join(dir, ".config", "opencode", "skills")
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(skillsDir, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s should be installed under the conventional dir: %v", s, err)
		}
	}
	cmds, _ := pack.CommandFiles()
	cmdsDir := filepath.Join(dir, ".config", "opencode", "commands")
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(cmdsDir, c)); !os.IsNotExist(err) {
			t.Errorf("command %s must NOT be installed with --with-skills only, stat err = %v", c, err)
		}
	}
}

func TestConfigureWithCommandsOnly(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--with-commands", "--json"}, &out); err != nil {
		t.Fatalf("configure --with-commands failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	cmds, _ := pack.CommandFiles()
	if !equalStrings(res.Installed["commands"], cmds) {
		t.Errorf("Installed[commands] = %v, want %v", res.Installed["commands"], cmds)
	}
	if _, ok := res.Installed["skills"]; ok {
		t.Errorf("Installed must not contain skills when only --with-commands, got %v", res.Installed)
	}
	cmdsDir := filepath.Join(dir, ".config", "opencode", "commands")
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(cmdsDir, c)); err != nil {
			t.Errorf("command %s should be installed: %v", c, err)
		}
	}
	// Sidecar: DELEGATION.txt (non-.md) next to the installed commands,
	// carrying exactly the active mapping table's RenderText output.
	sidecar, err := os.ReadFile(filepath.Join(cmdsDir, "DELEGATION.txt"))
	if err != nil {
		t.Fatalf("DELEGATION.txt sidecar must sit next to the commands: %v", err)
	}
	table, _ := pack.LoadMappingTable("opencode")
	want, _ := table.RenderText()
	if string(sidecar) != want {
		t.Errorf("sidecar content must equal RenderText output")
	}
	skills, _ := pack.SkillDirs()
	skillsDir := filepath.Join(dir, ".config", "opencode", "skills")
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(skillsDir, s)); !os.IsNotExist(err) {
			t.Errorf("skill %s must NOT be installed with --with-commands only, stat err = %v", s, err)
		}
	}
}

func TestConfigureWithAll(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--with-all", "--json"}, &out); err != nil {
		t.Fatalf("configure --with-all failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	skills, _ := pack.SkillDirs()
	cmds, _ := pack.CommandFiles()
	if !equalStrings(res.Installed["skills"], skills) {
		t.Errorf("Installed[skills] = %v, want %v", res.Installed["skills"], skills)
	}
	if !equalStrings(res.Installed["commands"], cmds) {
		t.Errorf("Installed[commands] = %v, want %v", res.Installed["commands"], cmds)
	}
	skillsDir := filepath.Join(dir, ".config", "opencode", "skills")
	cmdsDir := filepath.Join(dir, ".config", "opencode", "commands")
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(skillsDir, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s should be installed with --with-all: %v", s, err)
		}
	}
	for _, c := range cmds {
		data, err := os.ReadFile(filepath.Join(cmdsDir, c))
		if err != nil {
			t.Fatalf("command %s should be installed with --with-all: %v", c, err)
		}
		// Rendered frontmatter: description preserved, no provider keys invented (V2).
		text := string(data)
		if !strings.HasPrefix(text, "---\ndescription: ") {
			t.Errorf("installed command %s must carry rendered description-only frontmatter, got %.40q", c, text)
		}
		head := text[len("---\n"):strings.Index(text, "\n---\n")]
		if strings.Contains(head, "agent:") {
			t.Errorf("installed command %s must not invent an agent key (V2 omission)", c)
		}
	}
	if _, err := os.Stat(filepath.Join(cmdsDir, "DELEGATION.txt")); err != nil {
		t.Errorf("DELEGATION.txt sidecar expected next to the commands: %v", err)
	}
	// Also combining explicit flags should give same result
	dir2 := t.TempDir()
	var out2 bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir2, "--with-skills", "--with-commands", "--json"}, &out2); err != nil {
		t.Fatalf("configure --with-skills --with-commands failed: %v", err)
	}
	res2 := decodeConfigureResult(t, out2)
	if !equalStrings(res2.Installed["skills"], skills) || !equalStrings(res2.Installed["commands"], cmds) {
		t.Errorf("combined flags Installed = %v, want both", res2.Installed)
	}
}

func TestConfigureDryRunDefaultNoInstallNoTouch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--dry-run", "--json"}, &out); err != nil {
		t.Fatalf("configure dry-run default failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	if !res.DryRun {
		t.Error("dryRun must be true")
	}
	if len(res.Installed) != 0 {
		t.Errorf("dry-run default Installed = %v, want empty/absent", res.Installed)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("raw json: %v", err)
	}
	if _, ok := raw["installed"]; ok {
		t.Errorf("dry-run default raw JSON must not contain installed, got %s", raw["installed"])
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode.json")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not write opencode.json, stat err = %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create dir, stat err = %v", err)
	}
}

func TestConfigureDryRunWithSkills(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--dry-run", "--with-skills", "--json"}, &out); err != nil {
		t.Fatalf("configure dry-run --with-skills failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	skills, _ := pack.SkillDirs()
	if !equalStrings(res.Installed["skills"], skills) {
		t.Errorf("dry-run --with-skills Installed[skills] = %v, want %v", res.Installed["skills"], skills)
	}
	if _, ok := res.Installed["commands"]; ok {
		t.Errorf("dry-run --with-skills must not contain commands, got %v", res.Installed)
	}
	if res.Plan == "" {
		t.Error("dry-run must contain plan")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dry-run --with-skills must not create dir, stat err = %v", err)
	}
}

func TestConfigureDryRunWithAll(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--dry-run", "--with-all", "--json"}, &out); err != nil {
		t.Fatalf("configure dry-run --with-all failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	skills, _ := pack.SkillDirs()
	cmds, _ := pack.CommandFiles()
	if !equalStrings(res.Installed["skills"], skills) {
		t.Errorf("dry-run --with-all skills = %v, want %v", res.Installed["skills"], skills)
	}
	if !equalStrings(res.Installed["commands"], cmds) {
		t.Errorf("dry-run --with-all commands = %v, want %v", res.Installed["commands"], cmds)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dry-run --with-all must not create dir, stat err = %v", err)
	}
}

func TestConfigureTargetsStillWork(t *testing.T) {
	// opencode explicit vs default
	for _, target := range []string{"opencode", "claude", "codex"} {
		t.Run(target, func(t *testing.T) {
			// For claude/codex, isolate HOME to a temp dir so we don't pollute real home
			origHome := os.Getenv("HOME")
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			// Also need to handle USERPROFILE on some platforms? UserHomeDir uses HOME
			dir := t.TempDir()
			var out bytes.Buffer
			// opencode writes to dir; claude/codex write to HOME
			if err := run([]string{"configure", "--target", target, "--dir", dir, "--json"}, &out); err != nil {
				t.Fatalf("configure --target %s failed: %v", target, err)
			}
			res := decodeConfigureResult(t, out)
			if res.Target != target {
				t.Errorf("target = %q, want %q", res.Target, target)
			}
			if len(res.Installed) != 0 {
				t.Errorf("target %s default Installed = %v, want empty", target, res.Installed)
			}
			// Check file written at expected location
			if _, err := os.Stat(res.File); err != nil {
				t.Errorf("target %s file %q not written: %v", target, res.File, err)
			}
			// Restore HOME check: for claude/codex file should be under tmpHome
			if target != "opencode" && !strings.HasPrefix(res.File, tmpHome) {
				t.Errorf("target %s file = %q, want under HOME %q", target, res.File, tmpHome)
			}
			if target == "opencode" && res.File != filepath.Join(dir, "opencode.json") {
				t.Errorf("opencode file = %q, want %q", res.File, filepath.Join(dir, "opencode.json"))
			}
			// Ensure no skills installed to dir
			skills, _ := pack.SkillDirs()
			for _, s := range skills {
				if _, err := os.Stat(filepath.Join(dir, s)); !os.IsNotExist(err) {
					t.Errorf("skill %s must not be installed for target %s default, err=%v", s, target, err)
				}
			}
			_ = origHome
		})
	}
}

func TestConfigureWithAllTargets(t *testing.T) {
	// Ensure --with-all works for opencode/claude and still writes config.
	// codex refuses command installs (spike V3) — covered separately.
	for _, target := range []string{"opencode", "claude"} {
		t.Run(target+"_with_all", func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			dir := t.TempDir()
			var out bytes.Buffer
			if err := run([]string{"configure", "--target", target, "--dir", dir, "--with-all", "--json"}, &out); err != nil {
				t.Fatalf("configure --target %s --with-all failed: %v", target, err)
			}
			res := decodeConfigureResult(t, out)
			skills, _ := pack.SkillDirs()
			cmds, _ := pack.CommandFiles()
			if !equalStrings(res.Installed["skills"], skills) {
				t.Errorf("target %s --with-all skills = %v, want %v", target, res.Installed["skills"], skills)
			}
			if !equalStrings(res.Installed["commands"], cmds) {
				t.Errorf("target %s --with-all commands = %v, want %v", target, res.Installed["commands"], cmds)
			}
		})
	}
}

// TestConfigureClaudeLayout pins the claude conventional dirs and sidecar:
// <dir>/.claude/{skills,commands} with DELEGATION.txt next to the commands.
func TestConfigureClaudeLayout(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "claude", "--dir", dir, "--with-all", "--json"}, &out); err != nil {
		t.Fatalf("configure claude --with-all failed: %v", err)
	}
	cmdsDir := filepath.Join(dir, ".claude", "commands")
	skillsDir := filepath.Join(dir, ".claude", "skills")
	cmds, _ := pack.CommandFiles()
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(cmdsDir, c)); err != nil {
			t.Errorf("command %s must install to .claude/commands: %v", c, err)
		}
	}
	sidecar, err := os.ReadFile(filepath.Join(cmdsDir, "DELEGATION.txt"))
	if err != nil {
		t.Fatalf("DELEGATION.txt expected in .claude/commands: %v", err)
	}
	table, _ := pack.LoadMappingTable("claude")
	want, _ := table.RenderText()
	if string(sidecar) != want {
		t.Errorf("claude sidecar content must equal RenderText output")
	}
	skills, _ := pack.SkillDirs()
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(skillsDir, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s must install to .claude/skills: %v", s, err)
		}
	}
}

// TestConfigureCodexRefusesCommands (req R7): codex has no command target —
// --with-commands AND --with-all refuse deterministically before writing
// anything (not even the MCP config file).
func TestConfigureCodexRefusesCommands(t *testing.T) {
	for _, flags := range [][]string{{"--with-commands"}, {"--with-all"}} {
		t.Run(strings.Join(flags, ""), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "untouched")
			args := append([]string{"configure", "--target", "codex", "--dir", dir}, flags...)
			args = append(args, "--json")
			var out bytes.Buffer
			err := run(args, &out)
			if err == nil {
				t.Fatal("codex command install must refuse")
			}
			for _, want := range []string{"codex", "prompts", "0.117.0", "--with-skills"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal must mention %q, got: %v", want, err)
				}
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("refused run must not create anything, stat err = %v", err)
			}
		})
	}
}

// TestConfigureCodexSkillsSubtree (spike V3): codex installs ONLY skills,
// as a subtree under <dir>/.agents/skills, with DELEGATION.txt INSIDE the
// subtree — never a prompts path.
func TestConfigureCodexSkillsSubtree(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "codex", "--dir", dir, "--with-skills", "--json"}, &out); err != nil {
		t.Fatalf("configure codex --with-skills failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	skills, _ := pack.SkillDirs()
	if !equalStrings(res.Installed["skills"], skills) {
		t.Errorf("Installed[skills] = %v, want %v", res.Installed["skills"], skills)
	}
	skillsRoot := filepath.Join(dir, ".agents", "skills")
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(skillsRoot, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s must install under .agents/skills: %v", s, err)
		}
	}
	sidecar, err := os.ReadFile(filepath.Join(skillsRoot, "DELEGATION.txt"))
	if err != nil {
		t.Fatalf("DELEGATION.txt must sit inside the skills subtree: %v", err)
	}
	table, _ := pack.LoadMappingTable("codex")
	want, _ := table.RenderText()
	if string(sidecar) != want {
		t.Errorf("codex sidecar content must equal RenderText output")
	}
}

// TestConfigureDryRunActionsFidelity (req R7): dry-run prints exactly what
// would be written — full paths + create|overwrite|skip — while writing
// nothing at all.
func TestConfigureDryRunActionsFidelity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--dry-run", "--with-all", "--json"}, &out); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	res := decodeConfigureResult(t, out)
	if len(res.Changes) == 0 {
		t.Fatal("dry-run must report changes")
	}
	if res.Counts == nil || res.Counts.Created != len(res.Changes) {
		t.Fatalf("dry-run counts = %+v, want all-create over %d actions", res.Counts, len(res.Changes))
	}
	cmdsDir := filepath.Join(dir, ".config", "opencode", "commands")
	wantSidecar := filepath.Join(cmdsDir, "DELEGATION.txt")
	foundSidecar := false
	for _, a := range res.Changes {
		if a.Action != "create" {
			t.Errorf("fresh dry-run action = %q (%s), want create", a.Action, a.Path)
		}
		if !strings.HasPrefix(a.Path, dir) {
			t.Errorf("action path %q escapes base %q", a.Path, dir)
		}
		if a.Path == wantSidecar {
			foundSidecar = true
		}
	}
	if !foundSidecar {
		t.Errorf("sidecar %s missing from dry-run plan", wantSidecar)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create directories either, stat err = %v", err)
	}
}

// TestConfigureReinstallCountsAndScoping: second run overwrites nothing
// fresh (all skip), foreign files survive, counts report accurately.
func TestConfigureReinstallCountsAndScoping(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--with-all", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	cmdsDir := filepath.Join(dir, ".config", "opencode", "commands")
	foreign := filepath.Join(cmdsDir, "my-own.md")
	if err := os.WriteFile(foreign, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--with-all", "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	res := decodeConfigureResult(t, out)
	if res.Counts == nil {
		t.Fatal("reinstall must report counts")
	}
	if res.Counts.Created != 0 || res.Counts.Overwritten != 0 || res.Counts.Skipped != len(res.Changes) {
		t.Errorf("reinstall counts = %+v over %d actions, want all skip", res.Counts, len(res.Changes))
	}
	got, err := os.ReadFile(foreign)
	if err != nil || string(got) != "# mine\n" {
		t.Errorf("foreign file must survive reinstall untouched: %v %q", err, got)
	}
}

func TestConfigureMergePreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	// Seed opencode.json with another server
	seed := map[string]any{
		"mcp": map[string]any{
			"other": map[string]any{"type": "local", "command": []string{"/bin/other"}},
		},
		"custom": "keep-me",
	}
	b, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--json"}, &out); err != nil {
		t.Fatalf("configure merge failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		t.Fatalf("mcp missing")
	}
	if _, ok := mcp["other"]; !ok {
		t.Errorf("other server must be preserved, got %v", mcp)
	}
	if _, ok := mcp["eka"]; !ok {
		t.Errorf("eka entry must be present, got %v", mcp)
	}
	if cfg["custom"] != "keep-me" {
		t.Errorf("custom key must be preserved, got %v", cfg["custom"])
	}
	// Idempotency: second run should produce same file
	var out2 bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--json"}, &out2); err != nil {
		t.Fatalf("second configure failed: %v", err)
	}
	data2, _ := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if !bytes.Equal(data, data2) {
		t.Errorf("second configure should be idempotent: first %s second %s", data, data2)
	}
}

func TestConfigureJSONStability(t *testing.T) {
	// Ensure JSON output is stable and deterministic (sorted Installed, indented)
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--with-all", "--json"}, &out); err != nil {
		t.Fatalf("configure failed: %v", err)
	}
	// Must be valid JSON and round-trip stable
	var res configureResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	// Re-encode and compare Installed ordering (pack returns sorted)
	skills, _ := pack.SkillDirs()
	if !equalStrings(res.Installed["skills"], skills) {
		t.Errorf("skills not sorted/complete")
	}
}

// TestConfigureWriteHardening (td:configure-write-hardening): table-driven
// coverage of the hardened config-file writer — fresh write, overwrite of a
// tool-owned file, symlink refusal (link + target intact), and invalid-JSON
// refusal (file byte-untouched). Refusals must be deterministic errors (the
// CLI exits non-zero on them) and every case must leave no staging temp
// files behind.
func TestConfigureWriteHardening(t *testing.T) {
	hasEkaEntry := func(t *testing.T, file string) {
		t.Helper()
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("config file not written: %v", err)
		}
		var cfg map[string]any
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("config file must be valid JSON: %v\n%s", err, data)
		}
		mcp, ok := cfg["mcp"].(map[string]any)
		if !ok {
			t.Fatalf("mcp section missing: %v", cfg)
		}
		if _, ok := mcp["eka"]; !ok {
			t.Errorf("eka entry missing from mcp: %v", mcp)
		}
	}
	isRegular0644 := func(t *testing.T, file string) {
		t.Helper()
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			t.Errorf("written file must be regular, got mode %v", info.Mode())
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("written file perm = %v, want 0644", info.Mode().Perm())
		}
	}
	noTempLeftovers := func(t *testing.T, dir string) {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".ekacfg-") {
				t.Errorf("staging temp file left behind: %s", e.Name())
			}
		}
	}

	tests := []struct {
		name    string
		seed    func(t *testing.T, dir, file string)
		wantErr []string // required refusal substrings; empty = run must succeed
		verify  func(t *testing.T, dir, file string)
	}{
		{
			name: "fresh-write",
			verify: func(t *testing.T, dir, file string) {
				hasEkaEntry(t, file)
				isRegular0644(t, file)
			},
		},
		{
			name: "overwrite-pack-owned",
			seed: func(t *testing.T, dir, file string) {
				// First configure run makes the file tool-owned.
				var out bytes.Buffer
				if err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--json"}, &out); err != nil {
					t.Fatalf("seeding configure run failed: %v", err)
				}
			},
			verify: func(t *testing.T, dir, file string) {
				hasEkaEntry(t, file)
				isRegular0644(t, file)
			},
		},
		{
			name: "symlink-refusal",
			seed: func(t *testing.T, dir, file string) {
				real := filepath.Join(dir, "real-config.json")
				if err := os.WriteFile(real, []byte(`{"keep":true}`), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(real, file); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: []string{"refusing", "symlink", "nothing was modified"},
			verify: func(t *testing.T, dir, file string) {
				info, err := os.Lstat(file)
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("destination must remain a symlink, got mode %v", info.Mode())
				}
				got, err := os.ReadFile(file) // read through the link
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != `{"keep":true}` {
					t.Errorf("symlink target must stay byte-untouched, got %q", got)
				}
			},
		},
		{
			name: "invalid-json-refusal",
			seed: func(t *testing.T, dir, file string) {
				if err := os.WriteFile(file, []byte("{ not valid json"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: []string{"refusing", "not valid JSON", "left untouched"},
			verify: func(t *testing.T, dir, file string) {
				got, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "{ not valid json" {
					t.Errorf("invalid-JSON file must stay byte-untouched, got %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "opencode.json")
			if tt.seed != nil {
				tt.seed(t, dir, file)
			}
			var out bytes.Buffer
			err := run([]string{"configure", "--target", "opencode", "--dir", dir, "--json"}, &out)
			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("configure must succeed: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("configure must refuse deterministically")
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal must mention %q, got: %v", want, err)
					}
				}
			}
			if tt.verify != nil {
				tt.verify(t, dir, file)
			}
			noTempLeftovers(t, dir)
		})
	}
}
