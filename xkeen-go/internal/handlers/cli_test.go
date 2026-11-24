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
