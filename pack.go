// Package pack embeds the EKA AI Skill Pack and implements the
// installable artifact families the eka-mcp plugin exposes through the
// eka-cli plugin contract (plugin.Manifest / plugin.InstallResult).
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

	"github.com/maleolabs/eka-cli/plugin"
)

// Version is the eka-mcp plugin version (and the MCP server version it
// reports in serverInfo). It is the single version constant of the
// repository.
const Version = "0.1.0"

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

// Manifest is the plugin self-description under the extended plugin
// contract v1: plugin.Manifest plus the additive capabilities and
// source fields. The local plugin package (github.com/maleolabs/eka-cli
// v1.0.0) predates contract v1 and does not know these fields, so they
// are declared here — the emitted JSON is the contract.
//
// TODO(sto:mcp-manifest-capabilities): drop this local type (and the
// BuildManifest rename) once eka-cli >= v1.1 carries contract v1 —
// Capabilities/Source on plugin.Manifest — and BuildManifest can
// return plugin.Manifest again.
type Manifest struct {
	plugin.Manifest
	Capabilities []string `json:"capabilities"`
	Source       string   `json:"source"`
}

// BuildManifest builds the plugin self-description from the embedded
// skill pack: artifact entries are derived from the filesystem, so the
// manifest always reflects what the installer can actually install.
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
		},
		Capabilities: Capabilities,
		Source:       Source,
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
