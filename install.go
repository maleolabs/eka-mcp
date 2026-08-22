package pack

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Target-aware installation (req:agent-agnostic-skill-pack R5/R6/R7) ---
//
// Rendering and installation logic is owned by the pack-distribution
// vehicle (ADR-030 lineage): this package embeds the pack, the mappings/
// tables and the installer, so canonical bodies stay provider-neutral on
// disk while installed copies carry target-appropriate frontmatter and the
// active delegation table travels alongside as a NON-.md sidecar.

// SidecarName is the fixed file name of the delegation sidecar written next
// to the installed artifacts (req R5). It is deliberately NOT a .md file:
// legacy command directories scan every *.md as a command (spike V1), so a
// .md sidecar would surface as a phantom command.
const SidecarName = "DELEGATION.txt"

// InstallTargets lists the supported installation targets in deterministic
// order — the refusal messages, the docs and the tests share this list.
var InstallTargets = []string{"opencode", "claude", "codex"}

// TargetLayout is the conventional on-disk layout of one target ecosystem.
type TargetLayout struct {
	SkillsDir   string // root of the installed eka-* skill subtrees
	CommandsDir string // directory receiving the rendered eka-*.md commands; empty when the target has no command target
	SidecarDir  string // directory receiving DELEGATION.txt
}

// InstallBase resolves the anchor directory of a target's install tree: an
// explicit dir wins (project-scoped installs and tests); without one,
// opencode/claude fall back to the user home (global config convention,
// spike V3) and codex falls back to the process working directory
// (.agents/skills is a repository-local convention).
func InstallBase(target, dir string) (string, error) {
	if dir != "" {
		return expandHome(dir)
	}
	switch target {
	case "opencode", "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("pack: cannot resolve home dir for target %q: %w", target, err)
		}
		return home, nil
	case "codex":
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("pack: cannot resolve working directory for target %q: %w", target, err)
		}
		return cwd, nil
	default:
		return "", unsupportedTargetError(target)
	}
}

// ResolveLayout maps one target onto its conventional directories under the
// given base (spike V3):
//
//	opencode  <base>/.config/opencode/{skills,commands}   — sidecar next to the commands
//	claude    <base>/.claude/{skills,commands}            — sidecar next to the commands
//	codex     <base>/.agents/skills                       — sidecar inside the skills subtree;
//	            no command target (codex-cli removed ~/.codex/prompts in 0.117.0)
func ResolveLayout(target, base string) (TargetLayout, error) {
	switch target {
	case "opencode":
		root := filepath.Join(base, ".config", "opencode")
		return TargetLayout{
			SkillsDir:   filepath.Join(root, "skills"),
			CommandsDir: filepath.Join(root, "commands"),
			SidecarDir:  filepath.Join(root, "commands"),
		}, nil
	case "claude":
		root := filepath.Join(base, ".claude")
		return TargetLayout{
			SkillsDir:   filepath.Join(root, "skills"),
			CommandsDir: filepath.Join(root, "commands"),
			SidecarDir:  filepath.Join(root, "commands"),
		}, nil
	case "codex":
		skills := filepath.Join(base, ".agents", "skills")
		return TargetLayout{
			SkillsDir:  skills,
			SidecarDir: skills,
		}, nil
	default:
		return TargetLayout{}, unsupportedTargetError(target)
	}
}

// RenderCommand renders one embedded command file for a target ecosystem
// (req R3/R5): the canonical body stays byte-identical; the description-only
// frontmatter is rebuilt deterministically, preserving the description and
// adding provider-specific keys ONLY where they resolve from the active
// mapping table. Today no command-level key resolves for any target — the
// commands are orchestrators run by the primary agent, not mapped roles —
// so per spike V2 the renderer OMITS unresolvable keys instead of guessing.
// The embedded pack file itself is never mutated.
func RenderCommand(target, name string) ([]byte, error) {
	files, err := CommandFiles()
	if err != nil {
		return nil, err
	}
	if !contains(files, name) {
		return nil, fmt.Errorf("pack: unknown command %q", name)
	}
	data, err := fs.ReadFile(packFS, filepath.Join("commands", name))
	if err != nil {
		return nil, fmt.Errorf("pack: reading %s: %w", name, err)
	}
	description, body, err := splitCommandFrontmatter(name, data)
	if err != nil {
		return nil, err
	}
	keys, err := providerFrontmatterKeys(target)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "description: %s\n", description)
	for _, kv := range keys {
		fmt.Fprintf(&b, "%s: %s\n", kv[0], kv[1])
	}
	b.WriteString("---\n")
	b.WriteString(body)
	return []byte(b.String()), nil
}

// splitCommandFrontmatter splits an embedded command file into its
// single-line frontmatter description and the verbatim remainder (the body,
// including the blank line after the closing delimiter). The canonical
// contract (req R3) is description-only frontmatter: any other key is a
// pack defect and refuses loudly instead of being dropped silently.
func splitCommandFrontmatter(name string, data []byte) (string, string, error) {
	text := string(data)
	const open = "---\n"
	if !strings.HasPrefix(text, open) {
		return "", "", fmt.Errorf("pack: command %s has no frontmatter", name)
	}
	rest := text[len(open):]
	const closeDelim = "\n---\n"
	closing := strings.Index(rest, closeDelim)
	if closing < 0 {
		return "", "", fmt.Errorf("pack: command %s has unterminated frontmatter", name)
	}
	head := rest[:closing]
	body := rest[closing+len(closeDelim):]
	description := ""
	for _, line := range strings.Split(head, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if value, ok := strings.CutPrefix(trimmed, "description:"); ok {
			description = strings.TrimSpace(value)
			continue
		}
		return "", "", fmt.Errorf("pack: command %s has non-canonical frontmatter key in %q (canonical bodies are description-only, req R3)", name, line)
	}
	if description == "" {
		return "", "", fmt.Errorf("pack: command %s has no non-empty frontmatter description (canonical bodies are description-only, req R3)", name)
	}
	return description, body, nil
}

// providerFrontmatterKeys returns the ordered provider-specific frontmatter
// key/value pairs rendered into a command for one target. A key is added
// ONLY when it resolves from the active mapping table (req R5): today the
// commands are primary-agent orchestrators and no role row resolves to a
// command-level agent, so every target renders description-only (spike V2:
// omit rather than invent). This function is the single extension point for
// future targets whose mappings do resolve command-level keys.
func providerFrontmatterKeys(target string) ([][2]string, error) {
	switch target {
	case "opencode", "claude", "codex":
		return nil, nil
	default:
		return nil, unsupportedTargetError(target)
	}
}

// SidecarText returns the DELEGATION.txt content for one target: the active
// mapping table rendered as deterministic plain text (req R4/R5). Same
// table → same bytes, always.
func SidecarText(target string) (string, error) {
	table, err := LoadMappingTable(target)
	if err != nil {
		return "", err
	}
	return table.RenderText()
}

// FileAction is one planned or performed filesystem change of a target
// install. Action is one of:
//
//	create    — the file did not exist and is written;
//	overwrite — an existing regular pack-owned file is replaced;
//	skip      — nothing written (existing content already identical).
type FileAction struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

// ActionCounts summarizes the actions of one install run.
type ActionCounts struct {
	Created     int `json:"created"`
	Overwritten int `json:"overwritten"`
	Skipped     int `json:"skipped"`
}

// TargetInstallReport is the deterministic result of one target install:
// Files maps each artifact family to its installed entry names, Actions
// lists every filesystem change in path order, Counts aggregates them.
type TargetInstallReport struct {
	Files   map[string][]string `json:"files"`
	Actions []FileAction        `json:"actions"`
	Counts  ActionCounts        `json:"counts"`
}

// InstallForTarget installs the requested artifact families for one target
// ecosystem into the target's conventional directories (req R5/R6):
//
//   - skills   — pure copy of each eka-* subtree;
//   - commands — RENDERED copies (target frontmatter) of the eka-*.md bodies;
//   - sidecar  — DELEGATION.txt carrying the ACTIVE mapping table, written
//     next to the installed commands (opencode, claude) or inside the
//     installed skills subtree (codex), whenever any family is installed.
//
// Idempotent by construction: only pack-owned file NAMES are ever written;
// foreign files are never touched. Existing regular files with identical
// content are skipped. A symlinked or otherwise non-regular FINAL path
// component REFUSES with an error; intermediate directory symlinks are
// followed as usual (the dotfiles workflow). With dryRun nothing is created
// (not even directories) and the returned actions describe exactly what a
// real run would write.
func InstallForTarget(target, dir string, withSkills, withCommands, dryRun bool) (TargetInstallReport, error) {
	if !contains(InstallTargets, target) {
		return TargetInstallReport{}, unsupportedTargetError(target)
	}
	if withCommands && target == "codex" {
		return TargetInstallReport{}, fmt.Errorf("pack: target %q has no command directory (codex-cli removed ~/.codex/prompts in 0.117.0); install skills instead (--with-skills) — command-capable targets: opencode, claude", target)
	}
	base, err := InstallBase(target, dir)
	if err != nil {
		return TargetInstallReport{}, err
	}
	layout, err := ResolveLayout(target, base)
	if err != nil {
		return TargetInstallReport{}, err
	}

	type planEntry struct {
		path    string
		content []byte
	}
	var plan []planEntry
	files := map[string][]string{}

	if withSkills {
		dirs, err := SkillDirs()
		if err != nil {
			return TargetInstallReport{}, err
		}
		files["skills"] = dirs
		for _, s := range dirs {
			if err := safeName(s); err != nil {
				return TargetInstallReport{}, err
			}
			sub, err := fs.Sub(packFS, s)
			if err != nil {
				return TargetInstallReport{}, fmt.Errorf("pack: reading skill %s: %w", s, err)
			}
			walkErr := fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				data, err := fs.ReadFile(sub, p)
				if err != nil {
					return err
				}
				plan = append(plan, planEntry{path: filepath.Join(layout.SkillsDir, s, p), content: data})
				return nil
			})
			if walkErr != nil {
				return TargetInstallReport{}, fmt.Errorf("pack: reading skill %s: %w", s, walkErr)
			}
		}
	}

	if withCommands {
		cmds, err := CommandFiles()
		if err != nil {
			return TargetInstallReport{}, err
		}
		files["commands"] = cmds
		for _, c := range cmds {
			if err := safeName(c); err != nil {
				return TargetInstallReport{}, err
			}
			rendered, err := RenderCommand(target, c)
			if err != nil {
				return TargetInstallReport{}, err
			}
			plan = append(plan, planEntry{path: filepath.Join(layout.CommandsDir, c), content: rendered})
		}
	}

	if len(files) > 0 {
		sidecar, err := SidecarText(target)
		if err != nil {
			return TargetInstallReport{}, err
		}
		plan = append(plan, planEntry{path: filepath.Join(layout.SidecarDir, SidecarName), content: []byte(sidecar)})
	}

	sort.Slice(plan, func(i, j int) bool { return plan[i].path < plan[j].path })

	report := TargetInstallReport{Files: files}
	for _, e := range plan {
		action, err := classifyTarget(e.path, e.content)
		if err != nil {
			return TargetInstallReport{}, err
		}
		report.Actions = append(report.Actions, FileAction{Path: e.path, Action: action})
		switch action {
		case "create":
			report.Counts.Created++
		case "overwrite":
			report.Counts.Overwritten++
		case "skip":
			report.Counts.Skipped++
		}
	}
	if dryRun {
		return report, nil
	}
	for i, e := range plan {
		if report.Actions[i].Action == "skip" {
			continue
		}
		if err := writeFileScoped(e.path, e.content); err != nil {
			return TargetInstallReport{}, err
		}
	}
	return report, nil
}

// classifyTarget decides the action for one planned file WITHOUT writing:
// create when absent, skip when a regular file with identical content
// exists, overwrite when a regular file differs — and a hard refusal when
// the FINAL path component is a symlink or special file (Lstat never
// follows it). Intermediate directory symlinks are followed as usual.
func classifyTarget(path string, content []byte) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "create", nil
	}
	if err != nil {
		return "", fmt.Errorf("pack: cannot inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("pack: refusing to write %s: not a regular pack-owned file (symlink or special file)", path)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("pack: cannot read existing %s: %w", path, err)
	}
	if bytes.Equal(existing, content) {
		return "skip", nil
	}
	return "overwrite", nil
}

// writeFileScoped writes content to path so the FINAL path component is
// replaced, never written through: the payload is staged in a temporary
// file in the destination directory and renamed over the target, so even
// a racing symlink is replaced by the rename instead of being followed.
// Intermediate directory symlinks are followed as usual (the dotfiles
// workflow). Classification has already refused non-regular final
// components before this runs.
func writeFileScoped(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("pack: cannot create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".ekapack-*")
	if err != nil {
		return fmt.Errorf("pack: cannot stage %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("pack: cannot stage %s: %w", path, err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("pack: cannot stage %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("pack: cannot stage %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("pack: cannot write %s: %w", path, err)
	}
	return nil
}

// safeName rejects entry names that could escape the install directory.
// Names come from the embedded filesystem and are clean; this guard keeps
// that invariant load-bearing instead of assumed.
func safeName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, '\\') ||
		strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("pack: unsafe entry name %q", name)
	}
	return nil
}

// expandHome resolves a leading ~ to the user home directory and makes the
// result absolute — the only tilde handling the installer performs.
func expandHome(dir string) (string, error) {
	p := dir
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("pack: cannot resolve home dir: %w", err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("pack: cannot resolve dir %q: %w", dir, err)
	}
	return abs, nil
}

// unsupportedTargetError is the deterministic refusal for unknown targets.
func unsupportedTargetError(target string) error {
	return fmt.Errorf("pack: unsupported target %q (supported targets: %s)", target, strings.Join(InstallTargets, ", "))
}
