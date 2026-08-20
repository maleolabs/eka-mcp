#!/usr/bin/env bash
# Integration test: eka-mcp MCP server against a real opencode client.
#
# Builds the eka-mcp binary, registers it as a stdio MCP server in a
# throwaway opencode project, then drives a real opencode session that
# must call the eka "status" tool and report its JSON. The session
# continuing past the initialize handshake (opencode announces a newer
# protocol version than the 2024-11-05 baseline; the server echoes it)
# is the interop assertion — a broken handshake or framing kills the
# session before any tool call.
#
# Requirements: opencode on PATH (any recent 1.x), a model that needs no
# credentials (default: opencode/deepseek-v4-flash-free), go toolchain.
#
# Usage: scripts/integration-opencode.sh [--model <model>]
# Exit 0 on success, non-zero on failure.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODEL="${OPENCODE_MODEL:-opencode/deepseek-v4-flash-free}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> building eka-mcp"
(cd "$REPO_ROOT" && go build -o "$WORK/eka-mcp" ./cmd/eka-mcp)

echo "==> registering eka-mcp as an MCP server for opencode"
cat > "$WORK/opencode.json" <<EOF
{
  "\$schema": "https://opencode.ai/config.json",
  "mcp": {
    "eka": {
      "type": "local",
      "command": ["$WORK/eka-mcp", "serve"],
      "enabled": true
    }
  }
}
EOF

echo "==> running opencode session (model: $MODEL)"
PROMPT='Call the MCP tool "status" from the "eka" MCP server. Reply with exactly one line: STATUS_RESULT: followed by the raw JSON result of the tool call, nothing else.'
OUTPUT="$(cd "$WORK" && timeout 180 opencode run --model "$MODEL" "$PROMPT" 2>&1)" || {
  echo "FAIL: opencode run exited non-zero" >&2
  echo "$OUTPUT" >&2
  exit 1
}

echo "==> asserting the session reached the eka status tool"
if ! grep -q "STATUS_RESULT:" <<<"$OUTPUT"; then
  echo "FAIL: no STATUS_RESULT marker in opencode output" >&2
  echo "$OUTPUT" >&2
  exit 1
fi

# The marker must be followed by a JSON document (the status result).
JSON="$(sed -n 's/.*STATUS_RESULT://p' <<<"$OUTPUT" | head -1)"
if ! python3 -c 'import json,sys; json.loads(sys.argv[1])' "$JSON" 2>/dev/null; then
  echo "FAIL: STATUS_RESULT is not valid JSON" >&2
  echo "got: $JSON" >&2
  exit 1
fi

echo "PASS: opencode session called eka status and received JSON"
echo "status: $(python3 -c 'import json,sys; d=json.loads(sys.argv[1]); print({k: d[k] for k in ("SchemaVersion", "Objects") if k in d})' "$JSON")"