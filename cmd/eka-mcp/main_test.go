package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maleolabs/eka-cli/plugin"
	"github.com/maleolabs/eka-mcp"
)

// TestManifestJSON is the plugin contract test (a): "manifest --json"
// emits valid JSON whose skills/commands entries come from the embedded
// skill pack (the eka-* skill directories and the eka-*.md command
// files) — the manifest and the embedded filesystem can never drift.
func TestManifestJSON(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"manifest", "--json"}, &out); err != nil {
		t.Fatalf("manifest --json failed: %v", err)
	}

	var m plugin.Manifest
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("manifest --json must emit valid JSON: %v\n%s", err, out.String())
	}

	if m.Contract != plugin.ContractVersion {
		t.Errorf("contract = %q, want %q", m.Contract, plugin.ContractVersion)
	}
	if m.Name != "mcp" {
		t.Errorf("name = %q, want %q", m.Name, "mcp")
	}
	if m.Version != pack.Version {
		t.Errorf("version = %q, want %q", m.Version, pack.Version)
	}
	if m.Description == "" {
		t.Error("description must not be empty")
	}

	skills, err := pack.SkillDirs()
	if err != nil {
		t.Fatal(err)
	}
	commands, err := pack.CommandFiles()
	if err != nil {
		t.Fatal(err)
	}

	var gotSkills, gotCommands []string
	for _, a := range m.Artifacts {
		switch a.Kind {
		case "skills":
			gotSkills = a.Entries
		case "commands":
			gotCommands = a.Entries
		}
	}

	if len(skills) == 0 {
		t.Fatal("embedded skill pack must contain at least one eka-* skill directory")
	}
	if !equalStrings(gotSkills, skills) {
		t.Errorf("manifest skills entries = %v, want the embedded skill dirs %v", gotSkills, skills)
	}
	if !equalStrings(gotCommands, commands) {
		t.Errorf("manifest commands entries = %v, want the embedded command files %v", gotCommands, commands)
	}
	// Pin the known command files of the pack.
	if !contains(gotCommands, "eka-discuss.md") || !contains(gotCommands, "eka-execute.md") {
		t.Errorf("commands entries must include eka-discuss.md and eka-execute.md, got %v", gotCommands)
	}
	// Every skill entry must be an eka-* directory in the embedded FS.
	for _, s := range gotSkills {
		if !strings.HasPrefix(s, "eka-") {
			t.Errorf("skill entry %q must carry the eka- prefix", s)
		}
	}
}

// TestInstallSkills is the plugin contract test (b): "install skills
// --dir <tmp> --json" copies the eka-* skill subtrees into the target
// directory and reports them.
func TestInstallSkills(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"install", "skills", "--dir", dir, "--json"}, &out); err != nil {
		t.Fatalf("install skills --json failed: %v", err)
	}

	var res plugin.InstallResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("install --json must emit valid JSON: %v\n%s", err, out.String())
	}
	if res.Version != pack.Version {
		t.Errorf("version = %q, want %q", res.Version, pack.Version)
	}

	skills, err := pack.SkillDirs()
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(res.Installed, skills) {
		t.Errorf("installed = %v, want the embedded skill dirs %v", res.Installed, skills)
	}

	// Every skill directory must exist with its SKILL.md on disk.
	for _, s := range skills {
		skillDir := filepath.Join(dir, s)
		info, err := os.Stat(skillDir)
		if err != nil {
			t.Errorf("skill %s was not installed to %s: %v", s, skillDir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("skill %s is not a directory at %s", s, skillDir)
		}
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
			t.Errorf("skill %s has no SKILL.md at %s: %v", s, skillDir, err)
		}
	}
}

// TestInstallCommands installs the command files and verifies them on
// disk.
func TestInstallCommands(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"install", "commands", "--dir", dir, "--json"}, &out); err != nil {
		t.Fatalf("install commands --json failed: %v", err)
	}

	var res plugin.InstallResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("install --json must emit valid JSON: %v\n%s", err, out.String())
	}
	commands, err := pack.CommandFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(res.Installed, commands) {
		t.Errorf("installed = %v, want the embedded command files %v", res.Installed, commands)
	}
	for _, c := range commands {
		if _, err := os.Stat(filepath.Join(dir, c)); err != nil {
			t.Errorf("command %s was not installed to %s: %v", c, dir, err)
		}
	}
	// The installed command file must carry real content (the embedded
	// markdown), not an empty placeholder.
	data, err := os.ReadFile(filepath.Join(dir, commands[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		t.Errorf("installed command file %s must not be empty", commands[0])
	}
}

// TestInstallDryRun reports the plan without writing anything.
func TestInstallDryRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	var out bytes.Buffer
	if err := run([]string{"install", "skills", "--dir", dir, "--dry-run", "--json"}, &out); err != nil {
		t.Fatalf("install --dry-run --json failed: %v", err)
	}
	var res plugin.InstallResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("install --dry-run --json must emit valid JSON: %v\n%s", err, out.String())
	}
	skills, err := pack.SkillDirs()
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(res.Installed, skills) {
		t.Errorf("dry-run installed = %v, want %v", res.Installed, skills)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create the target directory, stat err = %v", err)
	}
}

// TestInstallUnknownKind refuses unknown artifact kinds.
func TestInstallUnknownKind(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"install", "bogus", "--dir", t.TempDir(), "--json"}, &out)
	if err == nil || !strings.Contains(err.Error(), "unknown artifact kind") {
		t.Errorf("install of an unknown kind must fail with the kind error, got %v", err)
	}
}

// TestBinaryPluginContract exercises the actual executable: build once,
// then drive "manifest --json" and "install skills --dir <tmp> --json"
// as subprocesses — the exact invocation the eka-cli plugin contract
// uses. The executable must talk pure JSON on stdout.
func TestBinaryPluginContract(t *testing.T) {
	bin := buildBinary(t)

	// manifest --json
	out := runBinary(t, bin, "manifest", "--json")
	var m plugin.Manifest
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("binary manifest --json must emit valid JSON: %v\n%s", err, out)
	}
	skills, err := pack.SkillDirs()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range m.Artifacts {
		if a.Kind == "skills" {
			got = a.Entries
		}
	}
	if !equalStrings(got, skills) {
		t.Errorf("binary manifest skills = %v, want %v", got, skills)
	}

	// install skills --dir <tmp> --json
	dir := t.TempDir()
	out = runBinary(t, bin, "install", "skills", "--dir", dir, "--json")
	var res plugin.InstallResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("binary install --json must emit valid JSON: %v\n%s", err, out)
	}
	if !equalStrings(res.Installed, skills) {
		t.Errorf("binary install installed = %v, want %v", res.Installed, skills)
	}
	for _, s := range skills {
		if _, err := os.Stat(filepath.Join(dir, s, "SKILL.md")); err != nil {
			t.Errorf("binary install did not copy %s/SKILL.md: %v", s, err)
		}
	}

	// install --dry-run must not write.
	empty := filepath.Join(t.TempDir(), "nope")
	out = runBinary(t, bin, "install", "skills", "--dir", empty, "--dry-run", "--json")
	var dry plugin.InstallResult
	if err := json.Unmarshal(out, &dry); err != nil {
		t.Fatalf("binary dry-run must emit valid JSON: %v\n%s", err, out)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Errorf("binary dry-run must not create the target, stat err = %v", err)
	}
}

// buildBinary compiles the eka-mcp executable once per test run.
func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "eka-mcp")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

// runBinary runs the executable with the given args and returns stdout.
func runBinary(t *testing.T, bin string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("binary %s %v failed: %v\n%s", bin, args, err, out)
	}
	return out
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

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
