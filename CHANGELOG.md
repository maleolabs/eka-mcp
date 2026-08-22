# Changelog

All notable changes to `eka-mcp` are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **BREAKING: `eka-mcp configure` decoupling (sto:mcp-configure-decoupling)** — `configure` now **only writes the MCP client config** by default; skill/command installation is opt-in via `--with-skills`, `--with-commands`, or `--with-all` (`--with-all` = both). Previously it always delegated `pack.Install("skills")` + `pack.Install("commands")`. Old callers (`eka-mcp configure --target opencode --dir . --json`) now get an `installed` field that is absent/empty unless opted in. Skills and templates remain accessible via MCP resources `eka://skills/*` and `eka://templates/*` without any file copy. `--dry-run` reflects only the opted-in plan and still touches no filesystem. Existing `--target opencode|claude|codex`, `--dir`, `--dry-run`, `--json` flags are unchanged. The write remains idempotent and merges without overwriting other servers.

## [1.0.0] - 2026-08-21

### Changed
- **Version bump to 1.0.0** — single version constant `pack.Version` (`pack.go`) and `anvil.yaml` both carry `1.0.0` (no `v` prefix, stamped via `ldflags -X github.com/maleolabs/eka-mcp.Version` at release). `README.md` version strings updated `0.1.0` → `1.0.0`.
- **Pack stabilization** — `skills/manifest.yaml` `pack.status` `experimental` → `stable`, `pack.version` `0.3.2` → `1.0.0`.
- **requiresEka refresh** — `skills/manifest.yaml` `requiresEka` `">= 0.6.0"` → `">= 1.0.0"` (floor for stable pack, current CLI `eka-cli` `1.2.3`; verified with `eka --help` / `anvil.yaml`).
- **Stale pack docs corrected (KMS gap fix)** — audited the four pack documents that previously referenced the removed `eka install` subject (commit `6600f38`, ADR-030 supersedes ADR-023):
  - `skills/manifest.yaml` `installCommand` verified to contain only the official `eka install skills|commands` forms (with subcommand).
  - `skills/README.md` status header `experimental (v0.1)` → `stable (v1.0.0)`; all `eka install` refs verified as official `eka install skills` / `eka install commands`.
  - `skills/commands/README.md` verified — no bare `eka install` remains, only official subcommand forms.
  - `skills/docs/installation.md` verified — no bare `eka install` remains, only official subcommand forms.
  - Grep gate: `grep -rn "eka install[^ s]"` on the four files now returns empty.

### Added
- This `CHANGELOG.md` (milestone `sto:mcp-versioning-pack` acceptance criterion).

### Milestone context
This release closes the `eka-mcp` 1.0 milestone, consolidating:
- Contract relocation (`eka-core/plugin` as canonical contract source, ADR-030)
- Command registration and hardening (conformance suite + fuzz harness)
- Capability expansion (full authoring surface)
- Per-agent `configure` UX (`--target opencode|claude|codex`, idempotent merge)
- Versioning-pack (this release — stable pack, version bump, docs correction)

[1.0.0]: https://github.com/maleolabs/eka-mcp/releases/tag/v1.0.0
