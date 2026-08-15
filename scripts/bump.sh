#!/bin/sh
# bump.sh — Version bump automation for EKA MCP
#
# Bumps the project version in anvil.yaml AND the pack.Version variable in
# pack.go (the two must stay in sync: anvil.yaml is what the Release
# workflow validates against, pack.Version is what the built binary
# reports in its manifest and MCP serverInfo), commits, pushes, creates
# a git tag (with 'v' prefix), and pushes the tag.
# Pushing the tag triggers the Release workflow (anvil pipeline ci →
# build → package → GitHub Release assets).
#
# Usage:
#   ./scripts/bump.sh patch    # 0.1.0 → 0.1.1
#   ./scripts/bump.sh minor    # 0.1.1 → 0.2.0
#   ./scripts/bump.sh major    # 0.2.0 → 1.0.0
#
# Prerequisites:
#   - anvil CLI installed (the version field is written via
#     `anvil project version set`)
#   - clean working tree on the branch to release
#
# This is an internal development tool, not part of the EKA MCP plugin.

set -eu

# ── Args ───────────────────────────────────────────────────────────
if [ $# -lt 1 ]; then
  echo "Usage: $0 <patch|minor|major>"
  exit 1
fi

BUMP_TYPE="$1"

case "$BUMP_TYPE" in
  patch|minor|major) ;;
  *)
    echo "Error: invalid bump type '$BUMP_TYPE'. Use: patch, minor, or major"
    exit 1
    ;;
esac

# ── Read current version from anvil.yaml ───────────────────────────
CURRENT=$(grep 'version:' anvil.yaml | head -1 | awk '{print $2}' | tr -d '"' | tr -d "'")
if [ -z "$CURRENT" ]; then
  echo "Error: could not read version from anvil.yaml"
  exit 1
fi

echo "Current version: $CURRENT"

# ── Calculate new version ──────────────────────────────────────────
MAJOR=$(echo "$CURRENT" | cut -d. -f1)
MINOR=$(echo "$CURRENT" | cut -d. -f2)
PATCH=$(echo "$CURRENT" | cut -d. -f3)

case "$BUMP_TYPE" in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch)
    PATCH=$((PATCH + 1))
    ;;
esac

NEW_VERSION="$MAJOR.$MINOR.$PATCH"
echo "New version:     $NEW_VERSION"

# ── Bump version in anvil.yaml via anvil ───────────────────────────
# anvil.yaml version field is without 'v' prefix
anvil project version set "$NEW_VERSION"

# ── Sync pack.go (single version source) ───────────────────────────
# pack.Version is a var (stamped via ldflags at release build time);
# the repository source still carries the version, and it must match
# anvil.yaml. Rewrite the declaration in place.
PACK_GO="pack.go"
if [ ! -f "$PACK_GO" ]; then
  echo "Error: $PACK_GO not found — aborting (nothing committed)"
  exit 1
fi
sed -i "s|^\(var Version = \).*|\1\"$NEW_VERSION\"|" "$PACK_GO"
if ! grep -q "var Version = \"$NEW_VERSION\"" "$PACK_GO"; then
  echo "Error: could not update pack.Version in $PACK_GO — aborting (nothing committed)"
  exit 1
fi
echo "pack.go Version synced: $NEW_VERSION"

# ── Commit ─────────────────────────────────────────────────────────
git add anvil.yaml pack.go
git commit -m "chore: bump version to $NEW_VERSION"

# ── Push to origin (current branch) ───────────────────────────────
BRANCH=$(git rev-parse --abbrev-ref HEAD)
echo "Pushing to origin/$BRANCH..."
git push origin "$BRANCH"

# ── Create tag and push ────────────────────────────────────────────
# anvil.yaml uses no 'v' prefix, git tag uses 'v' prefix
TAG="v$NEW_VERSION"
echo "Creating tag $TAG..."
git tag "$TAG"
git push origin "$TAG"

echo ""
echo "Done. $NEW_VERSION → $TAG — the Release workflow will publish the assets."
