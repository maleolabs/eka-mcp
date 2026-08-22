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

// TestSkillDescriptionFromFrontmatter (req:agent-agnostic-skill-pack
// R9): every embedded skill exposes a non-empty frontmatter description
// — the resource-listing description of eka://skills/<name> — and the
// value is exactly the SKILL.md frontmatter line.
func TestSkillDescriptionFromFrontmatter(t *testing.T) {
	dirs, err := SkillDirs()
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("the pack must embed at least one skill")
	}
	for _, name := range dirs {
		description, err := SkillDescription(name)
		if err != nil {
			t.Errorf("SkillDescription(%q): %v", name, err)
			continue
		}
		if description == "" {
			t.Errorf("SkillDescription(%q) must not be empty", name)
		}
		if strings.Contains(description, "\n") {
			t.Errorf("SkillDescription(%q) must be single-line, got %q", name, description)
		}
	}
	// Pin one known value so a frontmatter regression cannot pass silently.
	got, err := SkillDescription("eka-orientation")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "Use when working in an EKA-enabled project") {
		t.Errorf("eka-orientation description drifted, got %q", got)
	}
}

// TestSkillDescriptionUnknownRefused: unknown skill names refuse
// deterministically (the embedded filesystem is the source of truth).
func TestSkillDescriptionUnknownRefused(t *testing.T) {
	if _, err := SkillDescription("bogus"); err == nil {
		t.Fatal("unknown skill must error, got nil")
	}
}

// TestInstallUnknownKind refuses unknown kinds.
func TestInstallUnknownKind(t *testing.T) {
	_, err := Install("bogus", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "unknown artifact kind") {
		t.Fatalf("unknown kind must error, got %v", err)
	}
}

// --- Delegation mappings (req:agent-agnostic-skill-pack R4) ---

// TestMappingEcosystemsKnown pins the embedded delegation tables.
func TestMappingEcosystemsKnown(t *testing.T) {
	keys, err := MappingEcosystems()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "codex", "opencode"}
	if !reflect.DeepEqual(keys, want) {
		t.Errorf("MappingEcosystems = %v, want %v", keys, want)
	}
}

// TestEveryMappingCoversAllRoles: every embedded table resolves ALL 9
// roles of the closed vocabulary — no gaps, no extras — and every row
// is mode-consistent (the loader validates this; the test pins it).
func TestEveryMappingCoversAllRoles(t *testing.T) {
	keys, err := MappingEcosystems()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("no mapping tables embedded")
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			table, err := LoadMappingTable(key)
			if err != nil {
				t.Fatalf("LoadMappingTable(%q): %v", key, err)
			}
			if len(table.Roles) != len(RoleVocabulary) {
				t.Fatalf("table %q defines %d roles, want %d", key, len(table.Roles), len(RoleVocabulary))
			}
			for _, role := range RoleVocabulary {
				r, ok := table.Roles[role]
				if !ok {
					t.Errorf("table %q: role %q unresolved (gap)", key, role)
					continue
				}
				switch r.Mode {
				case ModeDelegate:
					if r.Agent == "" || r.Agent == SoloAgent {
						t.Errorf("table %q: role %q delegate needs a concrete agent, got %q", key, role, r.Agent)
					}
				case ModeSolo:
					if r.Agent != SoloAgent {
						t.Errorf("table %q: role %q solo must target %q, got %q", key, role, SoloAgent, r.Agent)
					}
				default:
					t.Errorf("table %q: role %q has unknown mode %q", key, role, r.Mode)
				}
			}
			for role := range table.Roles {
				if !containsString(RoleVocabulary, role) {
					t.Errorf("table %q: unknown role %q outside the closed vocabulary", key, role)
				}
			}
		})
	}
}

// TestOpenCodeDefaultReferenceRows pins the exact default reference
// mapping (req R4): opencode + maleolabs agents, including the variant
// and absorption notes.
func TestOpenCodeDefaultReferenceRows(t *testing.T) {
	table, err := LoadMappingTable(DefaultEcosystem)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]MappingRole{
		"architect":       {Agent: "alex-architect", Mode: ModeDelegate},
		"backend":         {Agent: "alex-backend", Mode: ModeDelegate, Note: "light variant: alex-backend-light"},
		"frontend":        {Agent: "alex-frontend", Mode: ModeDelegate, Note: "advanced UI variant: alex-frontend-ui"},
		"security-review": {Agent: "alex-security", Mode: ModeDelegate},
		"code-review":     {Agent: "alex-reviewer", Mode: ModeDelegate},
		"product-review":  {Agent: "althea-product-specialist", Mode: ModeDelegate, Note: "absorbs althea-ux-specialist and althea-review-specialist (R2)"},
		"qa":              {Agent: "alex-qa", Mode: ModeDelegate},
		"devops":          {Agent: "alex-devops", Mode: ModeDelegate},
		"documenter":      {Agent: "alex-documenter", Mode: ModeDelegate},
	}
	if !reflect.DeepEqual(table.Roles, want) {
		t.Fatalf("opencode reference rows drifted:\n got = %#v\nwant = %#v", table.Roles, want)
	}
}

// TestClaudeStockDefaultsToGeneralPurpose: the claude table must work on
// STOCK Claude Code without custom agent files — every role delegates to
// the built-in general-purpose agent (code.claude.com/docs/en/sub-agents).
func TestClaudeStockDefaultsToGeneralPurpose(t *testing.T) {
	table, err := LoadMappingTable("claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range RoleVocabulary {
		r := table.Roles[role]
		if r.Mode != ModeDelegate || r.Agent != "general-purpose" {
			t.Errorf("claude role %q = {%s %s}, want delegate/general-purpose (stock default)", role, r.Mode, r.Agent)
		}
	}
}

// TestCodexAllSolo: the codex table is deliberately ALL-SOLO (req R4 +
// ratified PM decision) — every role resolves to the explicit primary
// marker with mode solo, never a silent degrade.
func TestCodexAllSolo(t *testing.T) {
	table, err := LoadMappingTable("codex")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range RoleVocabulary {
		r := table.Roles[role]
		if r.Mode != ModeSolo || r.Agent != SoloAgent {
			t.Errorf("codex role %q = {%s %s}, want solo/primary (deliberate pack policy)", role, r.Mode, r.Agent)
		}
	}
}

// TestMappingRenderDeterministic: rendering is a pure function of the
// table — two renders are byte-identical, the output carries the table
// provenance and every role exactly once, in canonical order.
func TestMappingRenderDeterministic(t *testing.T) {
	for _, key := range []string{"opencode", "claude", "codex"} {
		t.Run(key, func(t *testing.T) {
			table, err := LoadMappingTable(key)
			if err != nil {
				t.Fatal(err)
			}
			a, err := table.RenderText()
			if err != nil {
				t.Fatal(err)
			}
			b, err := table.RenderText()
			if err != nil {
				t.Fatal(err)
			}
			if a != b {
				t.Fatalf("render must be deterministic for %q", key)
			}
			if !strings.Contains(a, "ecosystem: "+key) {
				t.Errorf("render must carry the ecosystem provenance, got:\n%s", a)
			}
			lines := strings.Split(strings.TrimRight(a, "\n"), "\n")
			body := lines[len(lines)-len(RoleVocabulary):]
			for i, role := range RoleVocabulary {
				if !strings.HasPrefix(body[i], role+" ") && body[i] != role {
					t.Errorf("render row %d = %q, want canonical order starting with %q", i, body[i], role)
				}
			}
			if key == "codex" && !strings.Contains(a, SoloAgent) {
				t.Errorf("codex render must carry the explicit %q marker", SoloAgent)
			}
		})
	}
}

// TestLoadUnknownEcosystemRefused: unknown ecosystem keys refuse
// deterministically (embedded filesystem is the source of truth).
func TestLoadUnknownEcosystemRefused(t *testing.T) {
	_, err := LoadMappingTable("bogus")
	if err == nil || !strings.Contains(err.Error(), "unknown mapping ecosystem") {
		t.Fatalf("unknown ecosystem must error, got %v", err)
	}
}

// TestMappingValidateRejectsBadTables pins the validation contract:
// gaps, extras, unknown modes, mode-inconsistent targets, multi-line
// fields and metadata mismatches all refuse loudly.
func TestMappingValidateRejectsBadTables(t *testing.T) {
	validRoles := func() map[string]MappingRole {
		roles := make(map[string]MappingRole, len(RoleVocabulary))
		for _, r := range RoleVocabulary {
			roles[r] = MappingRole{Agent: "some-agent", Mode: ModeDelegate}
		}
		return roles
	}
	valid := func() *MappingTable {
		return &MappingTable{
			Meta:  MappingMeta{Ecosystem: "test", Name: "test table", Source: "unit test"},
			Roles: validRoles(),
		}
	}
	cases := []struct {
		name    string
		mutate  func(*MappingTable)
		wantErr string
	}{
		{"missing role", func(tb *MappingTable) { delete(tb.Roles, "qa") }, "missing role"},
		{"extra role", func(tb *MappingTable) { tb.Roles["ux-review"] = MappingRole{Agent: "x", Mode: ModeDelegate} }, "closed"},
		{"unknown mode", func(tb *MappingTable) { tb.Roles["qa"] = MappingRole{Agent: "x", Mode: "auto"} }, "unknown mode"},
		{"delegate to primary marker", func(tb *MappingTable) { tb.Roles["qa"] = MappingRole{Agent: SoloAgent, Mode: ModeDelegate} }, "concrete agent target"},
		{"delegate to empty agent", func(tb *MappingTable) { tb.Roles["qa"] = MappingRole{Agent: "", Mode: ModeDelegate} }, "concrete agent target"},
		{"solo to named agent", func(tb *MappingTable) { tb.Roles["qa"] = MappingRole{Agent: "alex-qa", Mode: ModeSolo} }, `must resolve to "primary"`},
		{"multi-line note", func(tb *MappingTable) { tb.Roles["qa"] = MappingRole{Agent: "x", Mode: ModeDelegate, Note: "a\nb"} }, "single-line"},
		{"ecosystem mismatch", func(tb *MappingTable) { tb.Meta.Ecosystem = "other" }, "does not match table key"},
		{"empty source", func(tb *MappingTable) { tb.Meta.Source = "" }, "non-empty ecosystem, name and source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb := valid()
			tc.mutate(tb)
			err := tb.validate("test")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
	t.Run("valid baseline passes", func(t *testing.T) {
		if err := valid().validate("test"); err != nil {
			t.Fatalf("valid table must pass, got %v", err)
		}
	})
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
