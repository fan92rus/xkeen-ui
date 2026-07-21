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
	Type      string `json:"type"` // feat | fix | tweak
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

// GetFullChangelog returns the complete parsed changelog data (unreleased +
// all releases). Used by the GET /api/changelog endpoint.
func GetFullChangelog() (ChangelogData, error) {
	return changelog, nil
}
