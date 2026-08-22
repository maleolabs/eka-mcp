// Package pack embeds the EKA AI Skill Pack and implements the
// installable artifact families the eka-mcp plugin exposes through the
// eka-core plugin contract (plugin.Manifest / plugin.InstallResult).
//
// The embedded skills/ tree is the single source of truth for the
// manifest: entry names are derived from the embedded filesystem, never
// hardcoded duplicates — the manifest and the installed files can never
// drift apart.
package pack

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/maleolabs/eka-core/plugin"
)

// Version is the eka-mcp plugin version (and the MCP server version it
// reports in serverInfo). It is the single version source of the
// repository: anvil.yaml must carry the same value (scripts/bump.sh
// keeps both in sync), and the release pipeline stamps it via ldflags
// (-X github.com/maleolabs/eka-mcp.Version=<version>) so the released
// binary reports the tagged version in its manifest and serverInfo.
// The -X target is the module path (not .../pack): this package lives
// at the module root, so its import path IS the module path.
var Version = "1.1.3"

// Name is the stable plugin identity reported in the manifest.
const Name = "mcp"

// Description is the one-line plugin summary reported in the manifest.
const Description = "EKA MCP — the AI-agent integration layer: an MCP server over the EKA Runtime plus the EKA AI Skill Pack installer"

// Source is the canonical upstream repository of the plugin, reported
// in the manifest so registry/install tooling can track provenance.
const Source = "github.com/maleolabs/eka-mcp"

// Capabilities are the fixed capability tags of the plugin, reported
// in the manifest: it installs artifact families into the workspace
// ("install") and runs an MCP server ("mcp"). The tags are ordered
// and stable — the manifest is the contract. Immutable by convention:
// do not mutate or reorder (the JSON shape is the cross-repo contract
// with eka-cli).
var Capabilities = []string{"install", "mcp"}

// ManifestCommand is one B1 dispatch-protocol command declaration: the
// additive "commands" array on the v1 manifest contract (ADR-031). The
// contract version stays "v1" — the extension is backward compatible:
// old clients decoding into plugin.Manifest ignore the field, new
// clients (eka-cli B1 probe) decode into pluginCommandManifest and pick
// it up. The slice is stable and sorted by name.
type ManifestCommand struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Args        []string `json:"args"`
}

// Manifest is the extended plugin self-description: the v1 contract
// fields (via embedded plugin.Manifest) plus the B1 commands extension.
// It is the JSON shape the plugin emits for "manifest --json": the
// embedded Manifest carries contract, name, version, description,
// artifacts, capabilities and source, while Commands carries the
// deferred-registration dispatch table. Old clients decoding into
// plugin.Manifest see only the v1 fields (unknown "commands" ignored).
type Manifest struct {
	plugin.Manifest
	Commands []ManifestCommand `json:"commands,omitempty"`
}

// PluginCommands is the fixed B1 command set the plugin exposes via
// deferred registration (G1-G4). The single command "mcp" proxies the
// WHOLE binary: Args is empty, so every user argument passes through
// unchanged — the bare executable defaults to the MCP server, and
// "configure …", "install …" or "manifest --json" reach their
// subcommands directly. Pinning a fixed prefix (e.g. ["serve"]) would
// trap every invocation into that one subcommand. The plugin owns its
// flags (DisableFlagParsing in eka-cli).
var PluginCommands = []ManifestCommand{
	{Name: "mcp", Description: "EKA MCP server and plugin tooling", Args: []string{}},
}

// skillsFS embeds the EKA AI Skill Pack. The entry names of the
// manifest artifacts are read from this filesystem.
//
//go:embed skills
var skillsFS embed.FS

// packFS is the embedded filesystem rooted at the skills/ directory
// itself: "//go:embed skills" prefixes every path with "skills/", so the
// pack reads through a Sub filesystem to expose the pack root directly.
var packFS = mustSub(skillsFS, "skills")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// skillPrefix selects skill directories inside the embedded pack: every
// eka-* directory is one installable skill subtree.
const skillPrefix = "eka-"

// commandSuffix selects command files inside the embedded commands/
// directory: every eka-*.md file is one installable command.
const commandPrefix = "eka-"

// skillFile is the SKILL.md file name of every skill directory — the
// resource source of eka://skills/<name>.
const skillFile = "SKILL.md"

// templateDir is the drafts template directory inside the
// eka-knowledge-authoring skill — the resource source of
// eka://templates/<type>.
const templateDir = "eka-knowledge-authoring/templates/drafts"

// templateSuffix is the file name suffix of every draft template
// (the type token is the file name minus the suffix).
const templateSuffix = "-template.json"

// SkillDirs lists the embedded skill directory names (the eka-*
// directories at the skills/ root of the embedded filesystem), sorted.
// The names come from the embedded filesystem — the manifest and the
// installer share this list.
func SkillDirs() ([]string, error) {
	entries, err := fs.ReadDir(packFS, ".")
	if err != nil {
		return nil, fmt.Errorf("pack: cannot read embedded skills: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), skillPrefix) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// CommandFiles lists the embedded command file names (the eka-*.md
// files under commands/), sorted. The names come from the embedded
// filesystem — the manifest and the installer share this list.
func CommandFiles() ([]string, error) {
	entries, err := fs.ReadDir(packFS, "commands")
	if err != nil {
		return nil, fmt.Errorf("pack: cannot read embedded commands: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), commandPrefix) && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// SkillFile returns the SKILL.md content of one embedded skill — the
// read-only resource source of eka://skills/<name>. The name must be a
// known skill directory (the embedded filesystem is the source of
// truth, so an unknown or path-traversal name is refused
// deterministically).
func SkillFile(name string) ([]byte, error) {
	dirs, err := SkillDirs()
	if err != nil {
		return nil, err
	}
	if !contains(dirs, name) {
		return nil, fmt.Errorf("pack: unknown skill %q", name)
	}
	data, err := fs.ReadFile(packFS, filepath.Join(name, skillFile))
	if err != nil {
		return nil, fmt.Errorf("pack: skill %q has no %s: %w", name, skillFile, err)
	}
	return data, nil
}

// SkillDescription returns the frontmatter description of one embedded
// skill's SKILL.md — the resource-listing description of
// eka://skills/<name> (req:agent-agnostic-skill-pack R9: the resource
// carries the description from the frontmatter). The frontmatter is the
// `---`-delimited head of the file; the description is its single-line
// `description:` field. A skill without a parseable non-empty
// description refuses deterministically (callers degrade to their
// generic fallback).
func SkillDescription(name string) (string, error) {
	data, err := SkillFile(name)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", fmt.Errorf("pack: skill %q has no frontmatter", name)
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if value, ok := strings.CutPrefix(trimmed, "description:"); ok {
			description := strings.TrimSpace(value)
			if description == "" {
				return "", fmt.Errorf("pack: skill %q has an empty frontmatter description", name)
			}
			return description, nil
		}
	}
	return "", fmt.Errorf("pack: skill %q has no frontmatter description", name)
}

// TemplateTypes lists the draft template type tokens of the embedded
// pack (the *-template.json file names minus the suffix), sorted — the
// deterministic resource enumeration of eka://templates/<type>.
func TemplateTypes() ([]string, error) {
	entries, err := fs.ReadDir(packFS, templateDir)
	if err != nil {
		return nil, fmt.Errorf("pack: cannot read embedded templates: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, templateSuffix) {
			continue
		}
		out = append(out, strings.TrimSuffix(name, templateSuffix))
	}
	sort.Strings(out)
	return out, nil
}

// TemplateFile returns the draft template JSON of one type token — the
// read-only resource source of eka://templates/<type>. The token must
// be a known template type (the embedded filesystem is the source of
// truth, so an unknown token is refused deterministically).
func TemplateFile(typeToken string) ([]byte, error) {
	types, err := TemplateTypes()
	if err != nil {
		return nil, err
	}
	if !contains(types, typeToken) {
		return nil, fmt.Errorf("pack: no draft template for type %q", typeToken)
	}
	return fs.ReadFile(packFS, filepath.Join(templateDir, typeToken+templateSuffix))
}

// --- Delegation mappings (req:agent-agnostic-skill-pack R4) ---

// RoleVocabulary is the closed set of delegation roles shared by every
// mapping table (req R2): product-review absorbs UX-review duties, and
// backend/frontend cover their light/ui variants through row notes.
// The set is CLOSED — adding or removing a role is a breaking pack
// change (R12). The order below is the canonical pipeline order and the
// deterministic render order of every table.
var RoleVocabulary = []string{
	"architect",
	"backend",
	"frontend",
	"security-review",
	"code-review",
	"product-review",
	"qa",
	"devops",
	"documenter",
}

// Mapping resolution modes of a role row.
const (
	// ModeDelegate routes the role to a concrete named agent.
	ModeDelegate = "delegate"
	// ModeSolo records the explicit degrade path (req R8): the primary
	// agent performs the role inline — never silently.
	ModeSolo = "solo"
)

// SoloAgent is the explicit primary/solo marker of a solo row.
const SoloAgent = "primary"

// DefaultEcosystem is the default reference mapping key (req R4):
// opencode + maleolabs agents is the canonical resolution every other
// ecosystem table is measured against.
const DefaultEcosystem = "opencode"

// mappingSuffix selects mapping table files inside the embedded
// mappings/ directory: <ecosystem>.toml is one delegation table.
const mappingSuffix = ".toml"

// mappingsFS embeds the pre-rendered role→agent delegation tables, one
// TOML file per ecosystem, co-located with the embedded pack (req R4/R6).
//
//go:embed mappings
var mappingsFS embed.FS

// mappingFS is the embedded filesystem rooted at the mappings/ directory
// itself (same Sub pattern as packFS).
var mappingFS = mustSub(mappingsFS, "mappings")

// MappingMeta is the metadata header of one delegation table.
type MappingMeta struct {
	Ecosystem string `toml:"ecosystem"`
	Name      string `toml:"name"`
	Source    string `toml:"source"`
}

// MappingRole is one role→agent resolution row.
type MappingRole struct {
	Agent string `toml:"agent"` // concrete agent name, or SoloAgent when ModeSolo
	Mode  string `toml:"mode"`  // ModeDelegate or ModeSolo
	Note  string `toml:"note"`  // optional variant/absorption note
}

// MappingTable is one parsed delegation table: metadata plus exactly one
// row per RoleVocabulary role.
type MappingTable struct {
	Meta  MappingMeta            `toml:"meta"`
	Roles map[string]MappingRole `toml:"roles"`
}

// MappingEcosystems lists the embedded mapping table keys (the *.toml
// file names minus the suffix under mappings/), sorted — the
// deterministic enumeration of available ecosystems.
func MappingEcosystems() ([]string, error) {
	entries, err := fs.ReadDir(mappingFS, ".")
	if err != nil {
		return nil, fmt.Errorf("pack: cannot read embedded mappings: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, mappingSuffix) {
			continue
		}
		out = append(out, strings.TrimSuffix(name, mappingSuffix))
	}
	sort.Strings(out)
	return out, nil
}

// LoadMappingTable resolves the delegation table of one ecosystem key
// and returns it parsed and validated. The embedded filesystem is the
// source of truth, so an unknown ecosystem is refused deterministically;
// a table that does not cover the closed role vocabulary exactly is a
// pack defect and refuses loudly instead of degrading silently.
func LoadMappingTable(ecosystem string) (*MappingTable, error) {
	keys, err := MappingEcosystems()
	if err != nil {
		return nil, err
	}
	if !contains(keys, ecosystem) {
		return nil, fmt.Errorf("pack: unknown mapping ecosystem %q (have %v)", ecosystem, keys)
	}
	data, err := fs.ReadFile(mappingFS, ecosystem+mappingSuffix)
	if err != nil {
		return nil, fmt.Errorf("pack: reading mapping %q: %w", ecosystem, err)
	}
	var t MappingTable
	if err := toml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("pack: parsing mapping %q: %w", ecosystem, err)
	}
	if err := t.validate(ecosystem); err != nil {
		return nil, fmt.Errorf("pack: invalid mapping %q: %w", ecosystem, err)
	}
	return &t, nil
}

// validate enforces the table contract against the expected ecosystem
// key: complete metadata, exactly the closed role vocabulary (no gaps,
// no extras), known modes, mode-consistent agent targets, and
// single-line fields (the plain-text renderer relies on that).
func (t *MappingTable) validate(ecosystem string) error {
	if t.Meta.Ecosystem == "" || t.Meta.Name == "" || t.Meta.Source == "" {
		return fmt.Errorf("meta must define non-empty ecosystem, name and source")
	}
	if t.Meta.Ecosystem != ecosystem {
		return fmt.Errorf("meta.ecosystem %q does not match table key %q", t.Meta.Ecosystem, ecosystem)
	}
	for role := range t.Roles {
		if !contains(RoleVocabulary, role) {
			return fmt.Errorf("unknown role %q (the role vocabulary is closed)", role)
		}
	}
	for _, role := range RoleVocabulary {
		r, ok := t.Roles[role]
		if !ok {
			return fmt.Errorf("missing role %q", role)
		}
		switch r.Mode {
		case ModeDelegate:
			if r.Agent == "" || r.Agent == SoloAgent {
				return fmt.Errorf("role %q: mode %q needs a concrete agent target, got %q", role, ModeDelegate, r.Agent)
			}
		case ModeSolo:
			if r.Agent != SoloAgent {
				return fmt.Errorf("role %q: mode %q must resolve to %q, got %q", role, ModeSolo, SoloAgent, r.Agent)
			}
		default:
			return fmt.Errorf("role %q: unknown mode %q (want %q or %q)", role, r.Mode, ModeDelegate, ModeSolo)
		}
		for field, val := range map[string]string{"agent": r.Agent, "note": r.Note} {
			if strings.ContainsAny(val, "\r\n") {
				return fmt.Errorf("role %q: %s must be single-line", role, field)
			}
		}
	}
	return nil
}

// RenderText renders the table as deterministic plain text suitable for
// the DELEGATION.txt sidecar (req R5): a comment header carrying the
// table provenance, then one aligned row per role in canonical
// RoleVocabulary order. Same table → same bytes, always.
func (t *MappingTable) RenderText() (string, error) {
	if err := t.validate(t.Meta.Ecosystem); err != nil {
		return "", fmt.Errorf("pack: cannot render invalid mapping table: %w", err)
	}
	type row [4]string // role, mode, agent, note
	header := row{"role", "mode", "agent", "note"}
	rows := make([]row, 0, len(RoleVocabulary))
	for _, role := range RoleVocabulary {
		r := t.Roles[role]
		rows = append(rows, row{role, r.Mode, r.Agent, r.Note})
	}
	var widths [4]int
	for i, cell := range header {
		widths[i] = len(cell)
	}
	for _, r := range rows {
		for i, cell := range r {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# EKA delegation table — rendered from the embedded eka-mcp skill pack v%s\n", Version)
	fmt.Fprintf(&b, "# ecosystem: %s\n", t.Meta.Ecosystem)
	fmt.Fprintf(&b, "# name:      %s\n", t.Meta.Name)
	fmt.Fprintf(&b, "# source:    %s\n", t.Meta.Source)
	b.WriteString("# roles form the closed 9-role vocabulary of req:agent-agnostic-skill-pack (R2)\n")
	b.WriteString("\n")
	for _, r := range append([]row{header}, rows...) {
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %s",
			widths[0], r[0], widths[1], r[1], widths[2], r[2], r[3])
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteString("\n")
	}
	return b.String(), nil
}

// contains reports whether v is in the list.
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// BuildManifest builds the plugin self-description from the embedded
// skill pack: artifact entries are derived from the filesystem, so the
// manifest always reflects what the installer can actually install.
// The contract version stays "v1" (plugin.ContractVersion) — the B1
// "commands" array is additive: old clients decoding into
// plugin.Manifest ignore it, new clients (eka-cli B1 probe) pick it up.
// The returned Manifest embeds plugin.Manifest so the core contract
// stays authoritative, plus the Commands dispatch table.
func BuildManifest() (Manifest, error) {
	skills, err := SkillDirs()
	if err != nil {
		return Manifest{}, err
	}
	commands, err := CommandFiles()
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		Manifest: plugin.Manifest{
			Contract:    plugin.ContractVersion,
			Name:        Name,
			Version:     Version,
			Description: Description,
			Artifacts: []plugin.Artifact{
				{Kind: "skills", Entries: skills},
				{Kind: "commands", Entries: commands},
			},
			Capabilities: Capabilities,
			Source:       Source,
		},
		Commands: PluginCommands,
	}, nil
}

// Install installs one artifact family from the embedded skill pack
// into dir and returns the install result. Supported kinds:
//
//   - "skills"   — each eka-* directory installed as a subtree;
//   - "commands" — each command file installed as a single file.
//
// With dryRun the plan (the Installed list) is returned without
// touching the filesystem.
func Install(kind, dir string, dryRun bool) (plugin.InstallResult, error) {
	var names []string
	var err error
	switch kind {
	case "skills":
		names, err = SkillDirs()
	case "commands":
		names, err = CommandFiles()
	default:
		return plugin.InstallResult{}, fmt.Errorf("pack: unknown artifact kind %q (want \"skills\" or \"commands\")", kind)
	}
	if err != nil {
		return plugin.InstallResult{}, err
	}
	if !dryRun {
		if err := installTo(kind, dir, names); err != nil {
			return plugin.InstallResult{}, err
		}
	}
	return plugin.InstallResult{Installed: names, Version: Version}, nil
}

// installTo copies the named embedded entries of one kind into dir.
// Kind semantics: "skills" installs each entry as a directory subtree,
// "commands" installs each entry as a single file from commands/.
func installTo(kind, dir string, names []string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("pack: cannot create %s: %w", dir, err)
	}
	for _, name := range names {
		switch kind {
		case "skills":
			if err := copyTree(name, dir); err != nil {
				return fmt.Errorf("pack: installing skill %s: %w", name, err)
			}
		case "commands":
			src := filepath.Join("commands", name)
			data, err := fs.ReadFile(packFS, src)
			if err != nil {
				return fmt.Errorf("pack: reading %s: %w", src, err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				return fmt.Errorf("pack: writing %s: %w", filepath.Join(dir, name), err)
			}
		}
	}
	return nil
}

// copyTree copies one embedded directory subtree (rooted at the given
// skills-relative path) into dir/<root>, preserving the relative layout
// under the subtree root. Each skill installs as its own directory.
func copyTree(root, dir string) error {
	sub, err := fs.Sub(packFS, root)
	if err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, root, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(sub, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
