#!/bin/sh
# Setup script: installs git hooks for xkeen-ui development
# Run after cloning: sh xkeen-go/scripts/setup-hooks.sh

set -e

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
HOOKS_DIR="$REPO_ROOT/.git/hooks"

echo "Installing pre-commit hook..."
mkdir -p "$HOOKS_DIR"
cp "$REPO_ROOT/xkeen-go/scripts/pre-commit" "$HOOKS_DIR/pre-commit"
chmod +x "$HOOKS_DIR/pre-commit"

echo "✅ Pre-commit hook installed!"
echo ""
echo "The hook will run:"
echo "  - golangci-lint on staged .go files (if installed)"
echo "  - eslint on staged .js/.vue files (if node_modules exists)"
echo "  - go vet on staged .go files"
echo ""
echo "Bypass with: git commit --no-verify"
