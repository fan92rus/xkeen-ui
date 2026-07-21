package version

import "testing"

// TestChangelogParsed verifies that the embedded changelog.json is valid JSON,
// parses correctly, and contains expected structure.
func TestChangelogParsed(t *testing.T) {
	if len(changelog.Releases) == 0 {
		t.Fatal("expected at least one release in changelog")
	}

	// Each release must have version, date, changes
	for _, r := range changelog.Releases {
		if r.Version == "" {
			t.Error("release missing version")
		}
		if r.Date == "" {
			t.Errorf("release %s missing date", r.Version)
		}
		for _, c := range r.Changes {
			if !isValidType(c.Type) {
				t.Errorf("release %s: invalid change type %q", r.Version, c.Type)
			}
			if c.Text == "" {
				t.Errorf("release %s: empty change text", r.Version)
			}
		}
	}

	// Unreleased entries must be valid (if any)
	for _, c := range changelog.Unreleased {
		if !isValidType(c.Type) {
			t.Errorf("unreleased: invalid change type %q", c.Type)
		}
		if c.Text == "" {
			t.Error("unreleased: empty change text")
		}
	}
}

// TestGetFullChangelog verifies the exported getter returns the same data.
func TestGetFullChangelog(t *testing.T) {
	data, err := GetFullChangelog()
	if err != nil {
		t.Fatalf("GetFullChangelog error: %v", err)
	}
	if len(data.Releases) != len(changelog.Releases) {
		t.Error("GetFullChangelog returned different data")
	}
}

func isValidType(typ string) bool {
	return typ == "feat" || typ == "fix" || typ == "tweak"
}
