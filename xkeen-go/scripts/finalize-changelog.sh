#!/bin/sh
# finalize-changelog.sh — move unreleased entries to a versioned release.
# Called by CI (build.yml) before building the binary.
#
# Usage: finalize-changelog.sh <version> [changelog.json-path]
#
# Example: finalize-changelog.sh 0.8.0 internal/version/changelog.json

set -e

VERSION="${1:?version argument required}"
CHANGELOG="${2:-internal/version/changelog.json}"
DATE=$(date +%Y-%m-%d)

echo "📋 Finalizing changelog for version $VERSION..."

# Guard: skip if this version is already in releases (double-finalization protection)
EXISTING=$(jq --arg v "$VERSION" '.releases[] | select(.version == $v) | .version' "$CHANGELOG" 2>/dev/null || true)
if [ -n "$EXISTING" ]; then
    echo "⚠ Version $VERSION already finalized — skipping"
    exit 0
fi

# Check unreleased has entries
UNRELEASED_COUNT=$(jq '.unreleased | length' "$CHANGELOG")
if [ "$UNRELEASED_COUNT" -eq 0 ]; then
    echo "⚠ Warning: no unreleased entries to finalize"
    # Still create an empty release entry for the version
fi

# Move unreleased → releases[0] with version + date
jq --arg v "$VERSION" --arg d "$DATE" \
    '.releases = [{"version": $v, "date": $d, "changes": .unreleased}] + .releases
     | .unreleased = []' \
    "$CHANGELOG" > "$CHANGELOG.tmp" && mv "$CHANGELOG.tmp" "$CHANGELOG"

echo "✓ Finalized: $UNRELEASED_COUNT entries moved to release $VERSION"

# Generate GitHub release notes from the changelog
generate_release_notes() {
    jq -r --arg v "$VERSION" '
        .releases[0] |
        "## \($v)\n\n" +
        (.changes | group_by(.type) | map(
            "### " +
            (.[0].type | {"feat":"✨ Новое","fix":"🐛 Исправлено","tweak":"🔧 Мелкие правки"}[.]) + "\n" +
            (map("- " + .text + (if .important then " ⭐" else "" end)) | join("\n"))
        ) | join("\n\n"))
    ' "$CHANGELOG"
}

# If called with GENERATE_NOTES=1, output release notes to stdout
if [ "$GENERATE_NOTES" = "1" ]; then
    generate_release_notes
fi
