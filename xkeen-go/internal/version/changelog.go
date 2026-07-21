// Package version provides build version information and curated changelog.
package version

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed changelog.json
var changelogFS embed.FS

// ChangeEntry is a single human-readable changelog item.
type ChangeEntry struct {
	Type      string `json:"type"`      // feat | fix | tweak
	Text      string `json:"text"`
	Important bool   `json:"important"`
}

// ReleaseEntry is a versioned collection of changes.
type ReleaseEntry struct {
	Version string        `json:"version"`
	Date    string        `json:"date"`
	Changes []ChangeEntry `json:"changes"`
}

// ChangelogData is the full parsed changelog.json structure.
type ChangelogData struct {
	Unreleased []ChangeEntry  `json:"unreleased"`
	Releases   []ReleaseEntry `json:"releases"`
}

// ChangelogCategory groups changes by type for display.
type ChangelogCategory struct {
	Type    string        `json:"type"`
	Label   string        `json:"label"`
	Icon    string        `json:"icon"`
	Changes []ChangeEntry `json:"changes"`
}

// changelog holds the parsed data, loaded once at init.
var changelog ChangelogData

func init() {
	data, err := changelogFS.ReadFile("changelog.json")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded changelog.json: %v", err))
	}
	if err := json.Unmarshal(data, &changelog); err != nil {
		panic(fmt.Sprintf("failed to parse changelog.json: %v", err))
	}
}

// categoryMeta maps change types to display labels and icons.
var categoryMeta = []struct {
	Type  string
	Label string
	Icon  string
}{
	{"feat", "Новое", "✨"},
	{"fix", "Исправлено", "🐛"},
	{"tweak", "Мелкие правки", "🔧"},
}

// compareSemver compares two version strings. Returns -1 if a < b, 0 if equal,
// 1 if a > b. Versions are expected in "major.minor.patch" format (optionally
// with prerelease suffixes like "-dev.123"). Partial versions like "0.5" are
// zero-extended to "0.5.0". This is a best-effort comparison, not a strict
// semver parser — it does not handle build metadata (+build) or complex
// prerelease ordering rules.
func compareSemver(a, b string) int {
	// Strip prerelease suffix for comparison
	aCore := a
	bCore := b
	for i := 0; i < len(a); i++ {
		if a[i] == '-' {
			aCore = a[:i]
			break
		}
	}
	for i := 0; i < len(b); i++ {
		if b[i] == '-' {
			bCore = b[:i]
			break
		}
	}

	var aMaj, aMin, aPat, bMaj, bMin, bPat int
	_, _ = fmt.Sscanf(aCore, "%d.%d.%d", &aMaj, &aMin, &aPat)
	_, _ = fmt.Sscanf(bCore, "%d.%d.%d", &bMaj, &bMin, &bPat)

	if aMaj != bMaj {
		if aMaj < bMaj {
			return -1
		}
		return 1
	}
	if aMin != bMin {
		if aMin < bMin {
			return -1
		}
		return 1
	}
	if aPat != bPat {
		if aPat < bPat {
			return -1
		}
		return 1
	}
	return 0
}

// lastReleaseVersion returns the highest version string among releases,
// or "" if there are none.
func lastReleaseVersion() string {
	if len(changelog.Releases) == 0 {
		return ""
	}
	return changelog.Releases[0].Version // releases[0] is newest
}

// GetWhatsNew returns all ChangeEntry items for versions strictly greater than
// 'from' and less than or equal to 'to'. If 'to' is newer than the latest
// release, unreleased entries are also included. If 'from' is empty, all
// entries (all releases + unreleased) are returned.
func GetWhatsNew(from, to string) []ChangeEntry {
	var entries []ChangeEntry

	for _, r := range changelog.Releases {
		// Include release if from < version AND version <= to
		if from == "" || compareSemver(r.Version, from) > 0 {
			if compareSemver(r.Version, to) <= 0 {
				entries = append(entries, r.Changes...)
			}
		}
	}

	// If 'to' is newer than the latest release, include unreleased
	if last := lastReleaseVersion(); last == "" || compareSemver(to, last) > 0 {
		entries = append(entries, changelog.Unreleased...)
	}

	return entries
}

// GetWhatsNewGrouped returns changes grouped by category, in display order
// (feat → fix → tweak). Empty categories are omitted.
func GetWhatsNewGrouped(from, to string) []ChangelogCategory {
	entries := GetWhatsNew(from, to)
	if len(entries) == 0 {
		return nil
	}

	// Group by type
	byType := map[string][]ChangeEntry{}
	for _, e := range entries {
		byType[e.Type] = append(byType[e.Type], e)
	}

	// Build result in display order
	var result []ChangelogCategory
	for _, meta := range categoryMeta {
		if changes, ok := byType[meta.Type]; ok {
			result = append(result, ChangelogCategory{
				Type:    meta.Type,
				Label:   meta.Label,
				Icon:    meta.Icon,
				Changes: changes,
			})
		}
	}

	return result
}

// CompareVersions is an exported wrapper around compareSemver for use by
// other packages (handlers). Returns -1 if a < b, 0 if equal, 1 if a > b.
func CompareVersions(a, b string) int {
	return compareSemver(a, b)
}

// GetFullChangelog returns the complete parsed changelog data (unreleased +
// all releases). Used by the GET /api/changelog endpoint.
func GetFullChangelog() (ChangelogData, error) {
	return changelog, nil
}
