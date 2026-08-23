package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/maleolabs/eka-mcp"
)

// supportedTargets is the fixed set the configure subcommand understands —
// the pack's install targets (the pack owns rendering/installation per
// req:agent-agnostic-skill-pack R6; configure only delegates to it).
var supportedTargets = pack.InstallTargets

// configureOptions holds the parsed configure flags.
type configureOptions struct {
	Target       string
	Dir          string
	DryRun       bool
	JSON         bool
	WithSkills   bool
	WithCommands bool
	WithAll      bool
}

// configureResult is the machine-readable output of configure --json.
type configureResult struct {
	Target    string              `json:"target"`
	File      string              `json:"file"`
	Binary    string              `json:"binary"`
	Dir       string              `json:"dir"`
	DryRun    bool                `json:"dryRun"`
	Env       map[string]string   `json:"env,omitempty"`
	Entry     any                 `json:"entry"`
	Plan      string              `json:"plan,omitempty"`
	Installed map[string][]string `json:"installed,omitempty"`
	Changes   []pack.FileAction   `json:"changes,omitempty"`
	Counts    *pack.ActionCounts  `json:"counts,omitempty"`
}

// runConfigure implements "eka-mcp configure [--target opencode|claude|codex] [--dir <dir>] [--with-skills] [--with-commands] [--with-all] [--dry-run] --json".
func runConfigure(args []string, out io.Writer) error {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			fmt.Fprintln(out, "Usage: eka-mcp configure [--target opencode|claude|codex] [--dir <dir>] [--with-skills] [--with-commands] [--with-all] [--dry-run] --json")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Write the MCP client config entry for the target ecosystem (absolute binary path + EKA_HOME when set).")
			fmt.Fprintln(out, "By default only the MCP config is written; skills and commands are available via MCP resources")
			fmt.Fprintln(out, "eka://skills/* and eka://templates/* and require --with-skills, --with-commands or --with-all to also install.")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Install layout (conventional dirs; --dir anchors the tree for project-scoped installs):")
			fmt.Fprintln(out, "  opencode  <base>/.config/opencode/skills + .../commands (+ DELEGATION.txt sidecar next to the commands)")
			fmt.Fprintln(out, "  claude    <base>/.claude/skills + .../commands (+ DELEGATION.txt sidecar next to the commands)")
			fmt.Fprintln(out, "  codex     <base>/.agents/skills (+ DELEGATION.txt inside the skills subtree); commands are NOT")
			fmt.Fprintln(out, "            installable (codex-cli removed the prompts directory in 0.117.0) — --with-commands and")
			fmt.Fprintln(out, "            --with-all refuse deterministically.")
			fmt.Fprintln(out, "<base> is --dir, else the user home (opencode, claude) or the working directory (codex).")
			fmt.Fprintln(out, "Canonical bodies stay description-only in the pack; command frontmatter is rendered per target at")
			fmt.Fprintln(out, "install time. Re-installing overwrites only pack-owned files (foreign files are never touched);")
			fmt.Fprintln(out, "--dry-run prints paths + create|overwrite|skip and writes nothing.")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Config-file writes are safe by construction: the merged config is staged in a temporary file and")
			fmt.Fprintln(out, "renamed into place (atomic replace, never written through). A symlinked config destination is")
			fmt.Fprintln(out, "refused with the link left intact, and an existing config that is not valid JSON refuses with a")
			fmt.Fprintln(out, "clear error instead of being reset — the file stays byte-untouched in both cases.")
			return nil
		}
	}
	opts, err := parseConfigureArgs(args)
	if err != nil {
		return err
	}
	// Default target is opencode when not specified — documented default.
	if opts.Target == "" {
		opts.Target = "opencode"
	}
	if !isSupportedTarget(opts.Target) {
		return fmt.Errorf("configure: unsupported target %q (supported targets: %s)", opts.Target, strings.Join(supportedTargets, ", "))
	}
	// Deterministic refusal BEFORE anything is written: codex has no command
	// target (spike V3), so command installs refuse outright.
	if opts.Target == "codex" && (opts.WithCommands || opts.WithAll) {
		return errors.New(`configure: target "codex" cannot install commands: codex-cli removed the prompts directory (~/.codex/prompts) in 0.117.0; use --with-skills instead — command-capable targets: opencode, claude`)
	}
	if !opts.JSON {
		return errors.New("configure: missing --json")
	}

	// Resolve workspace dir: --dir or cwd or EKA_HOME fallback.
	dir, err := resolveConfigureDir(opts.Dir)
	if err != nil {
		return err
	}
	// Resolve absolute binary path.
	binary, err := resolveBinaryPath()
	if err != nil {
		return err
	}
	// Resolve EKA_HOME env normalization.
	env := map[string]string{}
	if v := os.Getenv("EKA_HOME"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil {
			v = abs
		}
		env["EKA_HOME"] = v
	}

	// Resolve target file and entry shape.
	file, entry, err := configureTargetFileAndEntry(opts.Target, dir, binary, env)
	if err != nil {
		return err
	}

	// Dry-run: print plan without touching filesystem.
	if opts.DryRun {
		res := configureResult{
			Target: opts.Target,
			File:   file,
			Binary: binary,
			Dir:    dir,
			DryRun: true,
			Env:    env,
			Entry:  entry,
			Plan:   fmt.Sprintf("would write MCP server entry %q to %s (binary %s)", "eka", file, binary),
		}
		// Include the install plan (dryRun) only for opted-in artifact families.
		// By default (no --with-*) nothing is installed; skills/commands are available via MCP resources
		// eka://skills/* and eka://templates/* and require explicit opt-in.
		// The pack computes the plan (paths + create|overwrite|skip) without touching disk.
		if opts.WithSkills || opts.WithCommands || opts.WithAll {
			rep, err := pack.InstallForTarget(opts.Target, opts.Dir, opts.WithSkills || opts.WithAll, opts.WithCommands || opts.WithAll, true)
			if err != nil {
				return fmt.Errorf("configure: planning install: %w", err)
			}
			res.Installed = rep.Files
			res.Changes = rep.Actions
			counts := rep.Counts
			res.Counts = &counts
		}
		return writeJSON(out, res)
	}

	// Write client config (merge, never overwrite other servers).
	if err := writeConfigureFile(opts.Target, file, entry); err != nil {
		return err
	}

	// Delegate skill/command install only when opted in.
	// Default configure only writes the MCP client config; skills and commands are accessible
	// via MCP resources (eka://skills/*, eka://templates/*) and require --with-skills / --with-commands / --with-all.
	// The pack owns rendering/installation (req R6): rendered command frontmatter per target plus
	// the DELEGATION.txt sidecar carrying the active mapping table.
	installed := map[string][]string{}
	var changes []pack.FileAction
	var counts *pack.ActionCounts
	if opts.WithSkills || opts.WithCommands || opts.WithAll {
		rep, err := pack.InstallForTarget(opts.Target, opts.Dir, opts.WithSkills || opts.WithAll, opts.WithCommands || opts.WithAll, false)
		if err != nil {
			return fmt.Errorf("configure: installing: %w", err)
		}
		installed = rep.Files
		changes = rep.Actions
		c := rep.Counts
		counts = &c
	}

	res := configureResult{
		Target:    opts.Target,
		File:      file,
		Binary:    binary,
		Dir:       dir,
		DryRun:    false,
		Env:       env,
		Entry:     entry,
		Installed: installed,
		Changes:   changes,
		Counts:    counts,
	}
	return writeJSON(out, res)
}

// parseConfigureArgs parses configure flags.
func parseConfigureArgs(args []string) (configureOptions, error) {
	var opts configureOptions
	seenJSON := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--target":
			if i+1 >= len(args) {
				return opts, errors.New("configure: --target requires a value")
			}
			i++
			opts.Target = args[i]
		case strings.HasPrefix(args[i], "--target="):
			opts.Target = strings.TrimPrefix(args[i], "--target=")
			if opts.Target == "" {
				return opts, errors.New("configure: --target requires a value")
			}
		case args[i] == "--dir":
			if i+1 >= len(args) {
				return opts, errors.New("configure: --dir requires a value")
			}
			i++
			opts.Dir = args[i]
		case strings.HasPrefix(args[i], "--dir="):
			opts.Dir = strings.TrimPrefix(args[i], "--dir=")
			if opts.Dir == "" {
				return opts, errors.New("configure: --dir requires a value")
			}
		case args[i] == "--dry-run":
			opts.DryRun = true
		case args[i] == "--json":
			seenJSON = true
			opts.JSON = true
		case args[i] == "--with-skills":
			opts.WithSkills = true
		case args[i] == "--with-commands":
			opts.WithCommands = true
		case args[i] == "--with-all":
			opts.WithAll = true
		default:
			return opts, fmt.Errorf("configure: unexpected argument %q", args[i])
		}
	}
	if !seenJSON {
		return opts, errors.New("configure: missing --json")
	}
	return opts, nil
}

func isSupportedTarget(t string) bool {
	for _, s := range supportedTargets {
		if s == t {
			return true
		}
	}
	return false
}

func resolveConfigureDir(dir string) (string, error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("configure: cannot resolve --dir %q: %w", dir, err)
		}
		return abs, nil
	}
	// Defaults to cwd; fallback to EKA_HOME if cwd unavailable.
	cwd, err := os.Getwd()
	if err == nil {
		return cwd, nil
	}
	if v := os.Getenv("EKA_HOME"); v != "" {
		abs, err := filepath.Abs(v)
		if err == nil {
			return abs, nil
		}
		return v, nil
	}
	return "", fmt.Errorf("configure: cannot resolve workspace dir: %w", err)
}

func resolveBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("configure: cannot resolve binary path: %w", err)
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return exe, nil
	}
	// Resolve symlink to absolute path for determinism.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, nil
}

// configureTargetFileAndEntry resolves the target file path and the MCP entry for the target.
func configureTargetFileAndEntry(target, dir, binary string, env map[string]string) (string, any, error) {
	switch target {
	case "opencode":
		file := filepath.Join(dir, "opencode.json")
		// opencode mcp entry: { type: local, command: [binary], enabled: true, environment: env }
		entry := map[string]any{
			"type":    "local",
			"command": []string{binary},
			"enabled": true,
		}
		if len(env) > 0 {
			entry["environment"] = env
		}
		return file, entry, nil
	case "claude":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, fmt.Errorf("configure: cannot resolve home dir: %w", err)
		}
		file := filepath.Join(home, ".claude.json")
		entry := map[string]any{
			"command": binary,
			"args":    []string{},
		}
		if len(env) > 0 {
			entry["env"] = env
		}
		return file, entry, nil
	case "codex":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", nil, fmt.Errorf("configure: cannot resolve home dir: %w", err)
		}
		// Spec says "~/.codex" entry — implement as JSON file at ~/.codex (home/.codex).
		// If ~/.codex is an existing directory, write to ~/.codex/config.json instead.
		candidate := filepath.Join(home, ".codex")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			candidate = filepath.Join(candidate, "config.json")
		}
		file := candidate
		entry := map[string]any{
			"command": binary,
			"args":    []string{},
		}
		if len(env) > 0 {
			entry["env"] = env
		}
		return file, entry, nil
	default:
		return "", nil, fmt.Errorf("configure: unsupported target %q (supported targets: %s)", target, strings.Join(supportedTargets, ", "))
	}
}

// writeConfigureFile merges the eka entry into the target file without
// overwriting other servers. Writing is hardened (td:configure-write-hardening):
//
//   - A symlinked or special-file destination is refused before anything
//     happens — the link itself is left byte-untouched (Lstat never follows
//     it). Intermediate directory symlinks are followed as usual.
//   - Existing content that is not valid JSON refuses deterministically
//     instead of silently resetting the file; the file stays byte-untouched.
//   - The payload is staged in a temporary file in the destination directory
//     and renamed over the target (CreateTemp 0600 → chmod → rename), so the
//     final path component is replaced, never written through — matching the
//     pack installer's writeFileScoped semantics.
func writeConfigureFile(target, file string, entry any) error {
	var topKey string
	switch target {
	case "opencode":
		topKey = "mcp"
	case "claude", "codex":
		topKey = "mcpServers"
	default:
		return fmt.Errorf("configure: unsupported target %q", target)
	}

	// Refuse non-regular final path components up front so a symlinked
	// config destination is never replaced or written through.
	if info, err := os.Lstat(file); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("configure: refusing to write %s: not a regular config file (symlink or special file); resolve or remove it first — nothing was modified", file)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("configure: cannot inspect %s: %w", file, err)
	}

	// Read existing file or start empty; unparseable JSON refuses instead
	// of resetting (the user's file is left exactly as-is).
	data := map[string]any{}
	if b, err := os.ReadFile(file); err == nil {
		if len(strings.TrimSpace(string(b))) > 0 {
			if err := json.Unmarshal(b, &data); err != nil {
				return fmt.Errorf("configure: refusing to rewrite %s: existing content is not valid JSON (%v); fix or remove the file first — it was left untouched", file, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("configure: cannot read %s: %w", file, err)
	}

	// Ensure topKey is a map.
	section, ok := data[topKey].(map[string]any)
	if !ok {
		// If topKey exists but is not an object, replace deterministically.
		section = map[string]any{}
	}
	section["eka"] = entry
	data[topKey] = section

	// Deterministic serialization with indent.
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("configure: cannot marshal %s: %w", file, err)
	}
	out = append(out, '\n')

	return writeConfigScoped(file, out)
}

// writeConfigScoped stages content in a temporary file inside the
// destination directory and renames it over path, so the FINAL path
// component is replaced atomically and never written through (a racing
// symlink would be replaced by the rename, not followed). The temporary
// file is created 0600 by CreateTemp, chmod-ed to 0644 to match the
// historical config-file mode, then renamed. Non-regular destinations are
// already refused by the caller before this runs.
func writeConfigScoped(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("configure: cannot create directory for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dir, ".ekacfg-*")
	if err != nil {
		return fmt.Errorf("configure: cannot stage %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("configure: cannot stage %s: %w", path, err)
	}
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("configure: cannot stage %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("configure: cannot stage %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("configure: cannot write %s: %w", path, err)
	}
	return nil
}
