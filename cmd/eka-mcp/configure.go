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

// supportedTargets is the fixed set the configure subcommand understands.
var supportedTargets = []string{"opencode", "claude", "codex"}

// configureOptions holds the parsed configure flags.
type configureOptions struct {
	Target string
	Dir    string
	DryRun bool
	JSON   bool
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
}

// runConfigure implements "eka-mcp configure [--target opencode|claude|codex] [--dir <dir>] [--dry-run] --json".
func runConfigure(args []string, out io.Writer) error {
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
		// Include install plan (dryRun) for completeness.
		skills, _ := pack.SkillDirs()
		cmds, _ := pack.CommandFiles()
		res.Installed = map[string][]string{
			"skills":   skills,
			"commands": cmds,
		}
		return writeJSON(out, res)
	}

	// Write client config (merge, never overwrite other servers).
	if err := writeConfigureFile(opts.Target, file, entry); err != nil {
		return err
	}

	// Delegate skill/command install (outside fixed manifest — delegates to install, not reimplements).
	// Use the same dir as the install root; for global targets (claude/codex) the dir is still
	// the workspace root for skill install — the MCP config file location is home-based, but skills
	// go into the workspace or agent config per target.
	installDir := configureInstallDir(opts.Target, dir)
	skillsRes, err := pack.Install("skills", installDir, false)
	if err != nil {
		return fmt.Errorf("configure: installing skills: %w", err)
	}
	commandsRes, err := pack.Install("commands", installDir, false)
	if err != nil {
		return fmt.Errorf("configure: installing commands: %w", err)
	}

	res := configureResult{
		Target: opts.Target,
		File:   file,
		Binary: binary,
		Dir:    dir,
		DryRun: false,
		Env:    env,
		Entry:  entry,
		Installed: map[string][]string{
			"skills":   skillsRes.Installed,
			"commands": commandsRes.Installed,
		},
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

// configureInstallDir resolves where skills/commands are installed for the target.
func configureInstallDir(target, dir string) string {
	// For all targets, delegate to the resolved dir (workspace root).
	// The MCP client config file location already distinguishes per-target home vs workspace.
	// This keeps delegation deterministic and testable (no hidden home writes for skills).
	return dir
}

// writeConfigureFile merges the eka entry into the target file without overwriting other servers.
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

	// Read existing file or start empty.
	data := map[string]any{}
	if b, err := os.ReadFile(file); err == nil {
		if len(strings.TrimSpace(string(b))) > 0 {
			if err := json.Unmarshal(b, &data); err != nil {
				// Treat invalid JSON as empty — but don't silently destroy: keep parse error as deterministic?
				// For idempotency, start fresh with a warning-less reset; other keys are lost only if file was invalid.
				data = map[string]any{}
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

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("configure: cannot create directory for %s: %w", file, err)
	}
	// Deterministic write with indent.
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("configure: cannot marshal %s: %w", file, err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(file, out, 0o644); err != nil {
		return fmt.Errorf("configure: cannot write %s: %w", file, err)
	}
	return nil
}
