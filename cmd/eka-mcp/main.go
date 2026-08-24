// Command eka-mcp is the EKA AI-agent integration layer executable.
//
// It serves two roles:
//
//  1. The eka-cli plugin contract (v1): "manifest --json" prints the
//     plugin self-description, "install <kind> --dir <dir> [--dry-run]
//     --json" installs an artifact family from the embedded skill pack.
//     Both are machine-readable (JSON on stdout), deterministic and
//     versioned — the CLI talks to this executable through exactly these
//     two subcommands (see github.com/maleolabs/eka-core/plugin).
//
//  2. The MCP server: "serve" (or no subcommand) runs the MCP server
//     over stdio (JSON-RPC 2.0, newline-delimited) exposing the EKA
//     Runtime capability layer as MCP tools and resources.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/maleolabs/eka-core/plugin"
	"github.com/maleolabs/eka-mcp"
	"github.com/maleolabs/eka-mcp/internal/eka"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "eka-mcp:", err)
		os.Exit(1)
	}
}

// run dispatches the subcommand. out receives the machine-readable
// output of the plugin subcommands (the MCP server writes to its own
// transport streams, not to out).
//
// B1 note: eka-cli's deferred registration dispatches `eka mcp` as
// `eka-mcp serve` with DisableFlagParsing (the plugin owns its flags).
// Extra args after "serve" are the user's args (e.g. `eka mcp --help`
// becomes `eka-mcp serve --help`), so "serve" must handle its own
// --help and not treat it as unknown.
func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return serve()
	}
	switch args[0] {
	case "manifest":
		return runManifest(args[1:], out)
	case "install":
		return runInstall(args[1:], out)
	case "configure":
		return runConfigure(args[1:], out)
	case "serve", "--stdio":
		// "--stdio" is the MCP client convention for "run the server
		// over stdio"; it is accepted as an alias of "serve".
		// The plugin owns its flags (B1 DisableFlagParsing), so handle
		// --help here instead of starting the server.
		for _, a := range args[1:] {
			if a == "--help" || a == "-h" || a == "help" {
				fmt.Fprintln(out, "Usage: eka-mcp serve [--stdio]")
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Run the EKA MCP server over stdio (JSON-RPC 2.0, newline-delimited).")
				fmt.Fprintln(out, "The server exposes the EKA Runtime capability layer as MCP tools and resources.")
				return nil
			}
		}
		return serve()
	default:
		return fmt.Errorf("unknown subcommand %q (want manifest, install, configure or serve)", args[0])
	}
}

// runManifest implements "manifest --json": print the plugin manifest.
func runManifest(args []string, out io.Writer) error {
	if len(args) != 1 || args[0] != "--json" {
		return errors.New("usage: eka-mcp manifest --json")
	}
	m, err := pack.BuildManifest()
	if err != nil {
		return err
	}
	return writeJSON(out, m)
}

// runInstall implements "install <kind> --dir <dir> [--dry-run] --json":
// install an artifact family from the embedded skill pack and print the
// install result.
func runInstall(args []string, out io.Writer) error {
	opts, err := parseInstallArgs(args)
	if err != nil {
		return err
	}
	res, err := pack.Install(opts.Kind, opts.Dir, opts.DryRun)
	if err != nil {
		return err
	}
	return writeJSON(out, res)
}

// parseInstallArgs parses the install subcommand flags: the first
// non-flag argument is the kind; --dir and --dry-run and --json are
// flags in any order.
func parseInstallArgs(args []string) (plugin.InstallOptions, error) {
	var opts plugin.InstallOptions
	seenJSON := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--dir":
			if i+1 >= len(args) {
				return opts, errors.New("install: --dir requires a value")
			}
			i++
			opts.Dir = args[i]
		case args[i] == "--dry-run":
			opts.DryRun = true
		case args[i] == "--json":
			seenJSON = true
		default:
			if opts.Kind != "" {
				return opts, fmt.Errorf("install: unexpected argument %q", args[i])
			}
			opts.Kind = args[i]
		}
	}
	if !seenJSON {
		return opts, errors.New("install: missing --json")
	}
	if opts.Kind == "" {
		return opts, errors.New("install: missing artifact kind (skills or commands)")
	}
	if opts.Dir == "" {
		return opts, errors.New("install: missing --dir")
	}
	return opts, nil
}

// writeJSON writes v as indented JSON with a trailing newline — the
// deterministic machine-readable output of the plugin contract.
func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// serve runs the MCP server over stdio. The capability layer opens the
// EKA Runtime read-only (runtime.Open: it never initializes a
// workspace), so the server starts cleanly even before a workspace
// exists — retrieval tools then report the uninitialized state.
//
// A startup failure is sanitized before it reaches stderr (which the
// MCP client captures): the same refusal-class policy as the MCP
// boundary — no workspace paths, no store details.
//
// Interactive notice: the server speaks newline-delimited JSON-RPC over
// stdio — stdout is the protocol stream and stays silent until a client
// sends requests. A human running "eka mcp" (or "eka-mcp serve")
// directly on a terminal would see nothing and reasonably conclude it
// is stuck, so the unicode banner is printed on STDERR (never stdout:
// that would corrupt the protocol framing) when stdin is a TTY. Non-TTY
// runs (a real MCP client, a pipe) print nothing. The banner is
// state-derived (version, pack manifest, workspace, capabilities) and
// deterministic — identical state yields identical bytes, no timestamps.
// Colors are emitted only when stderr is a TTY; otherwise plain bytes.
func serve() error {
	isStdinTTY := isatty.IsTerminal(os.Stdin.Fd())
	// Open the capability first so the banner can be state-derived. The
	// banner is best-effort: if the workspace cannot be opened, a
	// deterministic minimal banner is still shown on TTY before the
	// sanitized error is returned, and non-TTY stays silent.
	cap, err := eka.Open()
	if err != nil {
		if isStdinTTY {
			// Minimal banner without store state, still Deterministic,
			// never leaks paths (path replaced with <path> via sanitizer
			// if error contains one, but EKA_HOME is user-controlled and
			// safe to show as fallback? Use sanitized path or unknown.
			// For startup failure we show a fallback without workspace
			// counts — the error itself is sanitized.
			st := bannerState{
				Version:   pack.Version,
				Protocol:  mcp.ProtocolVersion,
				Transport: "stdio · JSON-RPC 2.0 · newline-delimited",
			}
			if info, ierr := pack.ReadPackInfo(); ierr == nil {
				st.PackName = info.Name
				st.PackVersion = info.Version
				st.PackStatus = info.Status
			} else {
				st.PackName = pack.Name
				st.PackVersion = pack.Version
				st.PackStatus = "stable"
			}
			st.WorkspacePath = "<unavailable>"
			st.ToolCount = mcp.ToolCount()
			if rc, rerr := mcp.ResourceCount(); rerr == nil {
				st.ResourceCount = rc
			}
			bs := &bannerStyle{Color: bannerColorEnabled(os.Stderr), W: os.Stderr}
			renderBanner(bs, st)
		}
		return fmt.Errorf("cannot open the EKA workspace: %s", mcp.SanitizeError(err))
	}
	defer cap.Close()
	if isStdinTTY {
		writeBannerIfTTY(os.Stderr, true, cap)
	}
	server := mcp.NewServer(cap)
	return server.Serve(os.Stdin, os.Stdout)
}
