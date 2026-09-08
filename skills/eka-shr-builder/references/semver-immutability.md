# Semver immutability

- 3-part `X.Y.Z`: major immutable (X must match on override/delete)
- 2-part `X.Y`: minor immutable (Y must match)
- Violation → `exit 1` (collision) or `exit 2` (usage)
- Override with `--force` for correction (delete respects but allows force)
- Source: `eka.yaml` project + `anvil.yaml` version, fallback `1.0.0`
- `sourceNamespace` derived via `resolveNewScope`, not hardcode `eka`
