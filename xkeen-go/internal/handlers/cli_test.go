package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

func TestCLICreateAndUp_Down(t *testing.T) {
	// Build a handler with a temp config dir + store next to "config.json"
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o750)
	cfg := &config.Config{AWGConfigDir: awgDir}
	configPath := filepath.Join(dir, "config.json")
	os.WriteFile(configPath, []byte("{}"), 0o600)

	h, err := NewCLIHandler(cfg, configPath)
	if err != nil {
		t.Fatalf("NewCLIHandler: %v", err)
	}
	// mock ndmc/keygen so it works off-router
	origKey, origLook, origRun := awgGenKeyFn, ndmcLookPathFn, ndmcRunFn
	t.Cleanup(func() { awgGenKeyFn, ndmcLookPathFn, ndmcRunFn = origKey, origLook, origRun })
	awgGenKeyFn = func() (string, string, error) { return "PRIV", "PUB", nil }
	ndmcLookPathFn = func(string) (string, error) { return "/bin/ndmc", nil }
	ndmcRunFn = func(args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "show interface") {
			return []byte("Interface, name = \"OpkgTun1\"\n"), nil
		}
		return nil, nil
	}

	out, err := CLICreateServer(h, "opkgtun1", "10.8.1.0/24", 4443, "full", nil)
	if err != nil {
		t.Fatalf("CLICreateServer: %v", err)
	}
	if !strings.Contains(out, "opkgtun1") {
		t.Fatalf("unexpected create output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(awgDir, "opkgtun1.conf")); err != nil {
		t.Fatalf("config not created: %v", err)
	}
	// Up/Down must not fail with "config not found" — the config was just
	// created and must be registered in the store for the next CLI process.
	if _, err := CLIUp(h, "opkgtun1"); err != nil && strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "binary not found") {
		t.Fatalf("CLIUp must find the freshly created config: %v", err)
	}
	// Down reaches the handler (no 404) — in the test env the up path may
	// fail because awg-quick is absent, which is expected off-router.
	if _, err := CLIDown(h, "opkgtun1"); err != nil && strings.Contains(err.Error(), "config") {
		t.Fatalf("CLIDown must find the freshly created config: %v", err)
	}
}

func TestCLICreateServer_CustomPresetParams(t *testing.T) {
	h, r, awgDir := newTestAWGHandler(t)
	origKey, origLook, origRun := awgGenKeyFn, ndmcLookPathFn, ndmcRunFn
	t.Cleanup(func() { awgGenKeyFn, ndmcLookPathFn, ndmcRunFn = origKey, origLook, origRun })
	awgGenKeyFn = func() (string, string, error) { return "PRIV", "PUB", nil }
	ndmcLookPathFn = func(string) (string, error) { return "", os.ErrNotExist }
	ndmcRunFn = func(_ ...string) ([]byte, error) { return nil, nil }
	_ = r

	out, err := CLICreateServer(h, "opkgtun4", "10.8.4.0/24", 4446, "custom", map[string]string{"Jc": "5", "H1": "42"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(out, "opkgtun4") {
		t.Fatalf("unexpected output: %s", out)
	}
	data, _ := os.ReadFile(filepath.Join(awgDir, "opkgtun4.conf"))
	if !strings.Contains(string(data), "Jc = 5") || !strings.Contains(string(data), "H1 = 42") {
		t.Fatalf("custom params missing:\n%s", data)
	}
}

func TestCLIWatchdog_PIDCheckAndMarker(t *testing.T) {
	h, r, awgDir := newTestAWGHandler(t)
	_ = r
	// temp dir for enabled markers
	origDir := serverEnabledDirFn
	t.Cleanup(func() { serverEnabledDirFn = origDir })
	serverEnabledDirFn = func() string { return awgDir }

	os.WriteFile(filepath.Join(awgDir, "opkgtun7.conf"), []byte("[Interface]\nListenPort = 4477\nAddress=10.8.7.1/24\n"), 0o600)
	if _, err := h.store.ScanAWGConfigs(awgDir); err != nil {
		t.Fatal(err)
	}
	origPID := opkgDaemonPIDFn
	defer func() { opkgDaemonPIDFn = origPID }()

	// (0, nil) = no daemon found — must be treated as DEAD
	opkgDaemonPIDFn = func(string) (int, error) { return 0, nil }
	// without an enabled marker, watchdog must not restart (intentionally down)
	out, err := CLIWatchdog(h)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "all") {
		t.Fatalf("intentionally-down server must be skipped: %s", out)
	}

	// with marker, dead daemon must be restarted (CLIUp attempted)
	markServerEnabled("opkgtun7", true)
	_, _ = CLIWatchdog(h) // will try real awg-quick (absent in test env) — just no panic
	if !isServerEnabled("opkgtun7") {
		t.Fatal("marker must persist until Down")
	}
	markServerEnabled("opkgtun7", false)
	if isServerEnabled("opkgtun7") {
		t.Fatal("marker must be cleared on Down")
	}
}

// Watchdog must restart a DEAD daemon (pid==0 with nil error) when the
// server is marked as wanted-up. Mutation pid>0 -> pid>=0 is caught here.
func TestCLIWatchdog_RestartsDeadDaemon(t *testing.T) {
	h, _, awgDir := newTestAWGHandler(t)
	origDir, origPID, origUp := serverEnabledDirFn, opkgDaemonPIDFn, awgQuickFn
	defer func() { serverEnabledDirFn, opkgDaemonPIDFn, awgQuickFn = origDir, origPID, origUp }()
	serverEnabledDirFn = func() string { return awgDir }
	opkgDaemonPIDFn = func(string) (int, error) { return 0, nil } // dead
	upCalls := 0
	awgQuickFn = func(string, string) ([]byte, error) { upCalls++; return nil, nil }

	os.WriteFile(filepath.Join(awgDir, "opkgtun8.conf"), []byte("[Interface]\nListenPort = 4488\nAddress=10.8.8.1/24\n"), 0o600)
	if _, err := h.store.ScanAWGConfigs(awgDir); err != nil {
		t.Fatal(err)
	}
	markServerEnabled("opkgtun8", true)

	out, err := CLIWatchdog(h)
	if err != nil {
		t.Fatal(err)
	}
	if upCalls == 0 || !strings.Contains(out, "opkgtun8") {
		t.Fatalf("dead daemon must be restarted; upCalls=%d out=%s", upCalls, out)
	}
}
