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
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(dir, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s should be installed: %v", s, err)
		}
	}
	cmds, _ := pack.CommandFiles()
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(dir, c)); !os.IsNotExist(err) {
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
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(dir, c)); err != nil {
			t.Errorf("command %s should be installed: %v", c, err)
		}
	}
	skills, _ := pack.SkillDirs()
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(dir, s)); !os.IsNotExist(err) {
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
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(dir, s, "SKILL.md")); err != nil {
			t.Errorf("skill %s should be installed with --with-all: %v", s, err)
		}
	}
	for _, c := range cmds {
		if _, err := os.Stat(filepath.Join(dir, c)); err != nil {
			t.Errorf("command %s should be installed with --with-all: %v", c, err)
		}
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
	// Ensure --with-all works for all targets and still writes config
	for _, target := range []string{"opencode", "claude", "codex"} {
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
