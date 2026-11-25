package pack

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

// TestMcpHelpDisclosureManifest (bug:mcp-help-subcommands-hidden) pins
// the disclosure surface: the manifest must list every actual
// cmd/eka-mcp subcommand (manifest, install, configure, serve) so
// `eka mcp -h` can disclose them. The mcp proxy remains the first entry.
func TestMcpHelpDisclosureManifest(t *testing.T) {
	m, err := BuildManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"manifest", "install", "configure", "serve"} {
		found := false
		for _, c := range m.Commands {
			if c.Name == want {
				found = true
				if c.Description == "" {
					t.Errorf("disclosure command %q must have description", want)
				}
				break
			}
		}
		if !found {
			t.Errorf("manifest disclosure must include %q, got %v", want, m.Commands)
		}
	}
	for _, want := range []string{"manifest", "install", "configure", "serve"} {
		found := false
		for _, c := range PluginCommands {
			if c.Name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("PluginCommands must include disclosure %q, got %v", want, PluginCommands)
		}
	}
	// Ensure the 4 disclosure commands match the actual binary subcommands.
	// The binary's run() handles manifest/install/configure/serve (plus --stdio alias for serve).
	wantSet := map[string]bool{"manifest": true, "install": true, "configure": true, "serve": true, "mcp": true}
	for _, c := range m.Commands {
		if !wantSet[c.Name] {
			t.Errorf("unexpected command %q in disclosure manifest, want only %v", c.Name, wantSet)
		}
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

// --- Mapping-vs-body drift guard (req:agent-agnostic-skill-pack M3) ---

// roleContractRoles extracts the role tokens of one command body's
// "## Role contract" section: the first column of the contract's markdown
// table, skipping the header and separator rows. The section format is
// stable pack canon; a missing section or table fails loudly instead of
// passing vacuously.
func roleContractRoles(t *testing.T, name, body string) []string {
	t.Helper()
	const heading = "## Role contract\n"
	start := strings.Index(body, heading)
	if start < 0 {
		t.Fatalf("%s has no %q section", name, strings.TrimSpace(heading))
	}
	rest := body[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	var roles []string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cell := strings.TrimSpace(strings.TrimPrefix(line, "|"))
		first := strings.TrimSpace(strings.SplitN(cell, "|", 2)[0])
		if first == "" || first == "Role" || strings.Trim(first, "-: ") == "" {
			continue // blank, header or separator row
		}
		roles = append(roles, first)
	}
	if len(roles) == 0 {
		t.Fatalf("%s: %q section carries no role table rows", name, strings.TrimSpace(heading))
	}
	return roles
}

// TestRoleContractMatchesMappingTables is the build-time consistency check
// (req M3): every role cited by a command body must resolve in EVERY
// mappings/*.toml table. Chosen approach (the robust one): instead of
// scanning free prose for role mentions, assert that RoleVocabulary ==
// the role contract listed in BOTH bodies' "## Role contract" sections
// (section extraction + role-token comparison), AND that every embedded
// table resolves exactly RoleVocabulary (loader-validated, pinned here).
// Drift in either direction — a body citing an unmapped role, a table
// row no body cites, a vocabulary change not propagated everywhere —
// fails loudly.
func TestRoleContractMatchesMappingTables(t *testing.T) {
	files, err := CommandFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no command files embedded")
	}
	want := append([]string(nil), RoleVocabulary...)
	sort.Strings(want)

	for _, name := range files {
		data, err := fs.ReadFile(packFS, filepath.Join("commands", name))
		if err != nil {
			t.Fatal(err)
		}
		got := roleContractRoles(t, name, string(data))
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s role contract drifted from RoleVocabulary:\n got = %v\nwant = %v", name, got, want)
		}
	}

	keys, err := MappingEcosystems()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		table, err := LoadMappingTable(key)
		if err != nil {
			t.Fatalf("LoadMappingTable(%q): %v", key, err)
		}
		tableRoles := make([]string, 0, len(table.Roles))
		for role := range table.Roles {
			tableRoles = append(tableRoles, role)
		}
		sort.Strings(tableRoles)
		if !reflect.DeepEqual(tableRoles, want) {
			t.Errorf("mapping %q resolves %v, want exactly the bodies' role contract %v", key, tableRoles, want)
		}
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

// --- Resource delivery (sto:mcp-resource-agent-delivery) ---

func TestManifestJSONCompact(t *testing.T) {
	data, err := ManifestJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("ManifestJSON must be valid JSON: %v", err)
	}
	if doc["schema"] != "eka-pack-manifest-v1" {
		t.Errorf("schema = %v, want eka-pack-manifest-v1", doc["schema"])
	}
	if doc["skills"] == nil || doc["templates"] == nil || doc["commands"] == nil {
		t.Error("manifest must carry skills/templates/commands")
	}
	skills := doc["skills"].([]any)
	if len(skills) == 0 {
		t.Error("manifest skills must not be empty")
	}
	if len(data) > 8000 {
		t.Errorf("manifest is not compact: %d bytes (want <8k)", len(data))
	}
	b, _ := ManifestJSON()
	if string(data) != string(b) {
		t.Error("ManifestJSON must be deterministic")
	}
}

func TestBootstrapContent(t *testing.T) {
	data, err := BootstrapContent()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"# EKA Bootstrap", "eka://manifest", "eka://skills/", "eka://templates/", "eka://commands/", "Fallback", "lazy", "versioned"} {
		if !strings.Contains(strings.ToLower(s), strings.ToLower(want)) && !strings.Contains(s, want) {
			t.Errorf("bootstrap must contain %q, got:\n%s", want, s[:600])
		}
	}
	if !strings.Contains(s, Version) {
		t.Errorf("bootstrap must mention the plugin version %q", Version)
	}
	a, _ := BootstrapContent()
	if string(a) != s {
		t.Error("BootstrapContent must be deterministic")
	}
}

func TestCommandFilesAndDescriptions(t *testing.T) {
	files, err := CommandFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 2 {
		t.Fatalf("want at least 2 commands, got %v", files)
	}
	for _, f := range files {
		data, err := CommandFile(f)
		if err != nil {
			t.Errorf("CommandFile(%q): %v", f, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("CommandFile(%q) empty", f)
		}
		desc, err := CommandDescription(f)
		if err != nil {
			t.Errorf("CommandDescription(%q): %v", f, err)
			continue
		}
		if desc == "" {
			t.Errorf("CommandDescription(%q) empty", f)
		}
	}
	if _, err := CommandFile("bogus.md"); err == nil {
		t.Error("unknown command must error")
	}
}

func TestAllResourceURIs(t *testing.T) {
	uris, err := AllResourceURIs()
	if err != nil {
		t.Fatal(err)
	}
	wantCount := 3 + len(mustSkillDirsForPackTest(t)) + len(mustTemplateTypesForPackTest(t)) + len(mustCommandFilesForPackTest(t))
	if len(uris) != wantCount {
		t.Fatalf("AllResourceURIs = %d, want %d (%v)", len(uris), wantCount, uris)
	}
	// Must contain manifest and bootstrap and status
	want := map[string]bool{ManifestURI: false, BootstrapURI: false, StatusURI: false}
	for _, u := range uris {
		if _, ok := want[u]; ok {
			want[u] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("AllResourceURIs missing %s", k)
		}
	}
	// Contains every skill/template/command
	for _, s := range mustSkillDirsForPackTest(t) {
		if !containsString(uris, SkillsPrefix+s) {
			t.Errorf("AllResourceURIs missing %s%s", SkillsPrefix, s)
		}
	}
}

func mustSkillDirsForPackTest(t *testing.T) []string {
	t.Helper()
	dirs, err := SkillDirs()
	if err != nil {
		t.Fatalf("SkillDirs: %v", err)
	}
	return dirs
}
func mustTemplateTypesForPackTest(t *testing.T) []string {
	t.Helper()
	types, err := TemplateTypes()
	if err != nil {
		t.Fatalf("TemplateTypes: %v", err)
	}
	return types
}
func mustCommandFilesForPackTest(t *testing.T) []string {
	t.Helper()
	files, err := CommandFiles()
	if err != nil {
		t.Fatalf("CommandFiles: %v", err)
	}
	return files
}

func TestParseVersionedURI(t *testing.T) {
	cases := []struct{ in, base, ver string }{
		{"eka://skills/eka-router", "eka://skills/eka-router", ""},
		{"eka://skills/eka-router@1.3.2", "eka://skills/eka-router", "1.3.2"},
		{"eka://manifest@1.3.2", "eka://manifest", "1.3.2"},
		{"eka://bootstrap", "eka://bootstrap", ""},
		{"eka://templates/adr@1.0.1", "eka://templates/adr", "1.0.1"},
	}
	for _, tc := range cases {
		b, v := ParseVersionedURI(tc.in)
		if b != tc.base || v != tc.ver {
			t.Errorf("ParseVersionedURI(%q) = (%q,%q), want (%q,%q)", tc.in, b, v, tc.base, tc.ver)
		}
	}
	if !IsCurrentVersion(Version) || !IsCurrentVersion("") {
		t.Error("IsCurrentVersion must accept plugin version and empty")
	}
	if IsCurrentVersion("9.9.9") {
		t.Error("IsCurrentVersion must reject unknown version")
	}
}

func TestResourceAnnotations(t *testing.T) {
	for _, uri := range []string{ManifestURI, BootstrapURI, StatusURI, SkillsPrefix + "eka-router", TemplatesPrefix + "adr", CommandsPrefix + "eka-discuss.md"} {
		ann := ResourceAnnotations(uri)
		if ann == nil || ann["priority"] == nil || ann["audience"] == nil {
			t.Errorf("ResourceAnnotations(%q) = %v, want priority and audience", uri, ann)
		}
	}
}
