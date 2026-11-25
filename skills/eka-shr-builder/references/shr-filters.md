# Shr filters (server-side)

Identity strict: `eka get <ns>/shr:<id> --level L0 --project X --version Y` → must match or `exit 2`.
Domain: `eka get operations --type shr --level L0 --project X --version Y --no-content`
MCP parity: tools `get`/`domain` with `level/project/version`.
Alias: `records → Operations`.
