package main

import (
	"strings"
	"testing"
)

// cronWatchdogMarker mirrors the const in install.go.
const testCronWatchdogMarker = "# xkeen-ui-watchdog (auto-managed)"

// filterCrontab reproduces the exact filtering logic from installCronWatchdog
// so we can unit-test it without a live crontab binary.
func filterCrontab(current, markerLine string) string {
	var kept []string
	for _, line := range strings.Split(current, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == markerLine {
			continue
		}
		if strings.Contains(line, "init.d") &&
			strings.Contains(line, "xkeen-ui") &&
			strings.Contains(line, "check") {
			continue
		}
		if trimmed != "" {
			kept = append(kept, line)
		}
	}
	result := strings.Join(kept, "\n")
	if result != "" {
		result += "\n"
	}
	result += markerLine + "\n" +
		"* * * * * /opt/etc/init.d/S70xkeen-ui check || /opt/etc/init.d/S70xkeen-ui start\n"
	return result
}

func TestFilterCrontab_PreservesLogRotation(t *testing.T) {
	input := `3 3 * * 0 /opt/sbin/xkeen -ug
*/5 * * * * /opt/sbin/router-monitor.sh
30 3 * * * /opt/bin/sh -c "for f in /opt/var/log/xray/access.log /opt/var/log/xray/error.log /opt/var/log/xkeen-ui.log /opt/var/log/nfqws2.log; do [ -s \"$f\" ] && cp \"$f\" \"$f.1\" && : > \"$f\"; done; find /opt/var/log -name \"*.log.*\" -mtime +7 -delete"
* * * * * /opt/etc/init.d/S99xkeen-ui check`
	result := filterCrontab(input, testCronWatchdogMarker)

	// Log rotation line MUST survive.
	if !strings.Contains(result, "xkeen-ui.log") {
		t.Fatal("log rotation line was deleted (contains xkeen-ui.log)")
	}
	if !strings.Contains(result, "router-monitor.sh") {
		t.Fatal("router-monitor line was deleted")
	}
	// Old watchdog (S99 check without start) MUST be removed.
	if strings.Contains(result, "S99xkeen-ui check") {
		t.Fatal("old S99 watchdog was not removed")
	}
	// New watchdog MUST be present.
	if !strings.Contains(result, "S70xkeen-ui check ||") {
		t.Fatal("new S70 watchdog was not added")
	}
}

func TestFilterCrontab_RemovesOldWatchdog(t *testing.T) {
	input := `* * * * * /opt/etc/init.d/S99xkeen-ui check`
	result := filterCrontab(input, testCronWatchdogMarker)

	if strings.Contains(result, "S99") {
		t.Fatalf("old S99 entry not removed: %s", result)
	}
	if !strings.Contains(result, "S70xkeen-ui check ||") {
		t.Fatal("new watchdog missing")
	}
}

func TestFilterCrontab_RemovesNewWatchdog(t *testing.T) {
	input := `# xkeen-ui-watchdog (auto-managed)
* * * * * /opt/etc/init.d/S70xkeen-ui check || /opt/etc/init.d/S70xkeen-ui start >> /opt/var/log/xkeen-ui.log 2>&1`
	result := filterCrontab(input, testCronWatchdogMarker)

	// Should not duplicate.
	if strings.Count(result, "S70xkeen-ui check") != 1 {
		t.Fatalf("expected exactly 1 watchdog, got: %s", result)
	}
	if strings.Count(result, testCronWatchdogMarker) != 1 {
		t.Fatalf("expected exactly 1 marker, got: %s", result)
	}
}

func TestFilterCrontab_EmptyCrontab(t *testing.T) {
	result := filterCrontab("", testCronWatchdogMarker)
	if !strings.Contains(result, "S70xkeen-ui check ||") {
		t.Fatalf("watchdog missing from empty crontab: %s", result)
	}
}

func TestFilterCrontab_PreservesAWGCrontab(t *testing.T) {
	// User might have an AWG-related crontab entry mentioning xkeen-ui dir.
	input := `* * * * * /opt/etc/init.d/S89awg-server check
*/5 * * * * /opt/sbin/router-monitor.sh`
	result := filterCrontab(input, testCronWatchdogMarker)

	if !strings.Contains(result, "S89awg-server") {
		t.Fatal("AWG crontab entry was deleted")
	}
	if !strings.Contains(result, "router-monitor.sh") {
		t.Fatal("router-monitor entry was deleted")
	}
}
