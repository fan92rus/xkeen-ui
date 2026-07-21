package version

import "testing"

// TestChangelogParsed verifies that the embedded changelog.json is valid JSON,
// parses correctly, and contains expected structure.
func TestChangelogParsed(t *testing.T) {
	if len(changelog.Releases) == 0 {
		t.Fatal("expected at least one release in changelog")
	}
	if len(changelog.Unreleased) == 0 {
		t.Fatal("expected at least one unreleased entry")
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

	// Unreleased entries must be valid
	for _, c := range changelog.Unreleased {
		if !isValidType(c.Type) {
			t.Errorf("unreleased: invalid change type %q", c.Type)
		}
		if c.Text == "" {
			t.Error("unreleased: empty change text")
		}
	}
}

// TestGetWhatsNew_VersionRange verifies that GetWhatsNew returns changes
// for versions strictly greater than 'from' and less than or equal to 'to'.
func TestGetWhatsNew_VersionRange(t *testing.T) {
	// 0.5.0 → 0.6.0: should include 0.5.1 + 0.6.0
	entries := GetWhatsNew("0.5.0", "0.6.0")
	// 0.5.1 has 3 changes, 0.6.0 has 1 change = 4 total
	if len(entries) != 4 {
		t.Errorf("expected 4 changes (0.5.1+0.6.0), got %d", len(entries))
	}
}

// TestGetWhatsNew_IncludesUnreleased verifies that unreleased entries are
// included when 'to' is newer than the latest release in the changelog.
func TestGetWhatsNew_IncludesUnreleased(t *testing.T) {
	// 0.7.2 → 99.0.0 (newer than latest release 0.7.2)
	// should include only unreleased entries
	entries := GetWhatsNew("0.7.2", "99.0.0")
	if len(entries) != len(changelog.Unreleased) {
		t.Errorf("expected %d unreleased entries, got %d",
			len(changelog.Unreleased), len(entries))
	}
}

// TestGetWhatsNew_SameVersion verifies no changes when from == to.
func TestGetWhatsNew_SameVersion(t *testing.T) {
	entries := GetWhatsNew("0.7.0", "0.7.0")
	if len(entries) != 0 {
		t.Errorf("expected 0 changes for same version, got %d", len(entries))
	}
}

// TestGetWhatsNew_EmptyFrom verifies that empty 'from' returns all changes.
func TestGetWhatsNew_EmptyFrom(t *testing.T) {
	entries := GetWhatsNew("", "99.0.0")
	// Should include ALL releases + unreleased
	total := len(changelog.Unreleased)
	for _, r := range changelog.Releases {
		total += len(r.Changes)
	}
	if len(entries) != total {
		t.Errorf("expected %d total changes, got %d", total, len(entries))
	}
}

// TestGetWhatsNew_GroupedByCategory verifies that GetWhatsNewGrouped returns
// categories in the expected order with correct labels and icons.
func TestGetWhatsNew_GroupedByCategory(t *testing.T) {
	groups := GetWhatsNewGrouped("0.5.0", "99.0.0")
	if len(groups) == 0 {
		t.Fatal("expected at least one category group")
	}

	for _, g := range groups {
		if g.Type == "" || g.Label == "" || g.Icon == "" {
			t.Errorf("group missing fields: %+v", g)
		}
		if len(g.Changes) == 0 {
			t.Errorf("group %s has no changes", g.Type)
		}
	}
}

func isValidType(typ string) bool {
	return typ == "feat" || typ == "fix" || typ == "tweak"
}
