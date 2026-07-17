package main

import (
	"strings"
	"testing"
)

func TestGetInitScript_CheckPreservesExitCode(t *testing.T) {
	script := getInitScript("xkeen-ui-keenetic-arm64")

	// The check) case must propagate the daemon's exit code, otherwise the
	// cron watchdog ("xkeen-ui check || xkeen-ui start") always sees exit 0
	// and never restarts the service after a crash.
	checkSection := extractSection(script, "check)")
	if !strings.Contains(checkSection, "exit $?") {
		t.Errorf("check) case must contain 'exit $?' to propagate exit code\nGot:\n%s", checkSection)
	}
}

func TestGetInitScript_UninstallPreservesExitCode(t *testing.T) {
	script := getInitScript("xkeen-ui-keenetic-arm64")

	uninstallSection := extractSection(script, "uninstall)")
	if !strings.Contains(uninstallSection, "exit $?") {
		t.Errorf("uninstall) case must contain 'exit $?' to propagate exit code\nGot:\n%s", uninstallSection)
	}
}

// extractSection returns the text between a case label (e.g. "check)") and
// the next ";;" terminator.
func extractSection(script, label string) string {
	idx := strings.Index(script, label)
	if idx < 0 {
		return ""
	}
	rest := script[idx:]
	end := strings.Index(rest, ";;")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
