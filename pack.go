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
var Version = "1.1.1"

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
// deferred registration (G1-G4). At least one: "mcp" dispatching to
// "serve" — the MCP server. The plugin owns its flags
// (DisableFlagParsing in eka-cli), so "serve" handles its own --help.
var PluginCommands = []ManifestCommand{
	{Name: "mcp", Description: "EKA MCP server", Args: []string{"serve"}},
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
