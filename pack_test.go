package pack

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEmbeddedPackPresent guards the embedded filesystem: the skill
// pack must actually be embedded (go:embed failed silently is a build
// error, but this pins the expected minimum surface).
func TestEmbeddedPackPresent(t *testing.T) {
	for _, want := range []string{"README.md", "manifest.yaml", "scripts/smoke-test.sh", "commands/eka-discuss.md"} {
		if _, err := fs.ReadFile(packFS, want); err != nil {
			t.Errorf("embedded pack must contain %s: %v", want, err)
		}
	}
}

// TestSkillDirsKnownSkills pins the expected eka-* skill set.
func TestSkillDirsKnownSkills(t *testing.T) {
	dirs, err := SkillDirs()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"eka-adoption",
		"eka-engineering-workflow",
		"eka-feedback",
		"eka-knowledge-authoring",
		"eka-knowledge-modification",
		"eka-knowledge-retrieval",
		"eka-knowledge-review",
		"eka-orientation",
		"eka-project-understanding",
		"eka-router",
		"eka-troubleshooting",
	} {
		if !containsString(dirs, want) {
			t.Errorf("SkillDirs must include %s, got %v", want, dirs)
		}
	}
}

// TestCommandFilesKnownCommands pins the expected command files.
func TestCommandFilesKnownCommands(t *testing.T) {
	files, err := CommandFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(files, "eka-discuss.md") || !containsString(files, "eka-execute.md") {
		t.Errorf("CommandFiles must include eka-discuss.md and eka-execute.md, got %v", files)
	}
}

// TestManifestDeterminism: two manifest builds are identical.
func TestManifestDeterminism(t *testing.T) {
	a, err := BuildManifest()
	if err != nil {
		t.Fatal(err)
	}
	b, err := BuildManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Artifacts) != 2 || len(b.Artifacts) != 2 {
		t.Fatalf("manifest must carry exactly the skills and commands artifacts, got %d/%d", len(a.Artifacts), len(b.Artifacts))
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("manifest builds must be fully identical, got:\na = %#v\nb = %#v", a, b)
	}
}

// TestManifestIncludesCommands pins the B1 dispatch-protocol extension:
// the manifest must declare at least the "mcp" command with EMPTY args
// — a whole-binary proxy (the bare executable defaults to the MCP
// server; every other subcommand passes through). The contract stays
// "v1" — the commands array is additive.
func TestManifestIncludesCommands(t *testing.T) {
	m, err := BuildManifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.Contract != "v1" {
		t.Errorf("contract = %q, want %q", m.Contract, "v1")
	}
	if len(m.Commands) == 0 {
		t.Fatal("manifest must declare at least one command (B1)")
	}
	found := false
	for _, c := range m.Commands {
		if c.Name == "mcp" {
			found = true
			if c.Description == "" {
				t.Error(`command "mcp" must have a non-empty description`)
			}
			if len(c.Args) != 0 {
				t.Errorf(`command "mcp" args = %v, want empty (whole-binary proxy)`, c.Args)
			}
		}
		if c.Name == "" || c.Description == "" {
			t.Errorf("command entries must have name and description, got %+v", c)
		}
	}
	if !found {
		t.Errorf("manifest must include command %q, got %v", "mcp", m.Commands)
	}
	if len(PluginCommands) == 0 || PluginCommands[0].Name != "mcp" {
		t.Errorf("PluginCommands must include %q as first entry, got %v", "mcp", PluginCommands)
	}
}

// TestInstallCommandsPreservesContent: the installed command file
// byte-matches the embedded file.
func TestInstallCommandsPreservesContent(t *testing.T) {
	dir := t.TempDir()
	res, err := Install("commands", dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed) == 0 {
		t.Fatal("install commands must install something")
	}
	got, err := os.ReadFile(filepath.Join(dir, res.Installed[0]))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := fs.ReadFile(packFS, filepath.Join("commands", res.Installed[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(embedded) {
		t.Errorf("installed %s differs from the embedded file", res.Installed[0])
	}
}

// TestInstallSkillsSubtree: the full subtree of a skill directory lands
// on disk (SKILL.md plus any nested resources, e.g. templates).
func TestInstallSkillsSubtree(t *testing.T) {
	dir := t.TempDir()
	res, err := Install("skills", dir, false)
	if err != nil {
		t.Fatal(err)
	}
	foundAuthoring := false
	for _, s := range res.Installed {
		if s == "eka-knowledge-authoring" {
			foundAuthoring = true
			if _, err := os.Stat(filepath.Join(dir, s, "templates", "README.md")); err != nil {
				t.Errorf("eka-knowledge-authoring templates must be installed as a subtree: %v", err)
			}
		}
	}
	if !foundAuthoring {
		t.Fatal("eka-knowledge-authoring must be in the installed set")
	}
}

// TestInstallUnknownKind refuses unknown kinds.
func TestInstallUnknownKind(t *testing.T) {
	_, err := Install("bogus", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "unknown artifact kind") {
		t.Fatalf("unknown kind must error, got %v", err)
	}
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
