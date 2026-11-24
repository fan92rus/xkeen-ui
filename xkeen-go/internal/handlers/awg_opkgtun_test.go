package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// OpkgTun attach mode: KeeneticOS (5.x) owns the netdev (created via
// `interface OpkgTun0` in the NDMS config). xkeen-ui must attach amneziawg-go
// to the existing netdev instead of running awg-quick (which would try to
// create/delete the interface and fight with NDMS).

func setupOpkgTest(t *testing.T) {
	t.Helper()
	origLink, origProc, origExec, origLook, origSleep := opkgLinkExistsFn, opkgProcessRunningFn, opkgExecFn, opkgLookPathFn, opkgSleepFn
	t.Cleanup(func() {
		opkgLinkExistsFn, opkgProcessRunningFn, opkgExecFn, opkgLookPathFn, opkgSleepFn = origLink, origProc, origExec, origLook, origSleep
	})
	opkgSleepFn = func(time.Duration) {}
}

func TestIsOpkgTunAttachMode(t *testing.T) {
	setupOpkgTest(t)

	linkExists := false
	opkgLinkExistsFn = func(string) bool { return linkExists }

	if isOpkgTunAttachMode("opkgtun0") {
		t.Fatal("attach mode must require the netdev to exist")
	}
	linkExists = true
	if !isOpkgTunAttachMode("opkgtun0") {
		t.Fatal("opkgtun0 with existing netdev must be attach mode")
	}
	if isOpkgTunAttachMode("server") {
		t.Fatal("non-opkgtun names are never attach mode")
	}
}

func TestStartOpkgTunAttach(t *testing.T) {
	setupOpkgTest(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "opkgtun0.conf")
	conf := "[Interface]\nAddress = 10.8.0.1/24\nDNS = 192.168.1.1\nListenPort = 443\nMTU = 1420\nJc = 8\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands [][]string
	started := false
	procRunning := false
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	opkgProcessRunningFn = func(string) bool { return procRunning }
	opkgLinkExistsFn = func(string) bool { return true }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		full := append([]string{name}, args...)
		commands = append(commands, full)
		switch {
		case name == "awg" && args[0] == "show" && len(args) == 2:
			return []byte("interface: opkgtun0\n  listening port: 55962\n"), nil
		case name == "awg" && args[0] == "setconf":
			procRunning = true
		case name == "pkill":
			procRunning = false
		}
		return nil, nil
	}
	// capture daemon start via package hook
	origStart := opkgStartDaemonFn
	t.Cleanup(func() { opkgStartDaemonFn = origStart })
	opkgStartDaemonFn = func(_, _ string) error {
		started = true
		procRunning = true
		return nil
	}

	if err := startOpkgTunAttach("opkgtun0", confPath); err != nil {
		t.Fatalf("startOpkgTunAttach: %v", err)
	}
	if !started {
		t.Fatal("daemon was not started")
	}

	joined := joinCommands(commands)
	// setconf must be called with a runtime file that has no Address/DNS/MTU
	setIdx := strings.Index(joined, "awg setconf opkgtun0")
	if setIdx < 0 {
		t.Fatalf("setconf not called; got:\n%s", joined)
	}
	// listen port mismatch (55962 != 443) must trigger awg set listen-port + re-setconf
	if !strings.Contains(joined, "awg set opkgtun0 listen-port 443") {
		t.Fatalf("listen-port enforcement missing; got:\n%s", joined)
	}
	if strings.Count(joined, "awg setconf opkgtun0") != 2 {
		t.Fatalf("expected setconf twice (initial + after listen-port); got:\n%s", joined)
	}

	// runtime conf must strip NDMS-owned keys — capture it from the setconf
	// call (the file is deleted after use, it holds the private key)
	var runtimeConf string
	for _, cmdline := range commands {
		joinedCmd := strings.Join(cmdline, " ")
		if strings.HasPrefix(joinedCmd, "awg setconf opkgtun0 ") {
			runtimeConf = strings.TrimPrefix(joinedCmd, "awg setconf opkgtun0 ")
		}
	}
	if runtimeConf == "" {
		t.Fatal("setconf not called")
	}
	data, err := os.ReadFile(runtimeConf)
	if err != nil {
		// file already removed — expected in production path; re-create via
		// runtimeConfForOpkgTun to validate stripping
		p, perr := runtimeConfForOpkgTun("opkgtun0", confPath)
		if perr != nil {
			t.Fatalf("runtime conf not written: %v", perr)
		}
		defer os.Remove(p)
		if data, err = os.ReadFile(p); err != nil {
			t.Fatalf("re-read runtime conf: %v", err)
		}
	}
	for _, banned := range []string{"Address", "DNS", "MTU"} {
		if strings.Contains(string(data), banned) {
			t.Errorf("runtime conf must not contain %s:\n%s", banned, data)
		}
	}
	if !strings.Contains(string(data), "ListenPort = 443") || !strings.Contains(string(data), "Jc = 8") {
		t.Errorf("runtime conf lost server params:\n%s", data)
	}
	_ = os.Remove(runtimeConf)
}

func TestStartOpkgTunAttach_ListenPortOK(t *testing.T) {
	setupOpkgTest(t)

	dir := t.TempDir()
	confPath := filepath.Join(dir, "opkgtun0.conf")
	if err := os.WriteFile(confPath, []byte("[Interface]\nListenPort = 443\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var commands [][]string
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	opkgProcessRunningFn = func(string) bool { return true } // already running
	opkgLinkExistsFn = func(string) bool { return true }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		if name == "awg" && args[0] == "show" {
			return []byte("  listening port: 443\n"), nil
		}
		return nil, nil
	}
	origStart := opkgStartDaemonFn
	t.Cleanup(func() { opkgStartDaemonFn = origStart })
	opkgStartDaemonFn = func(string, string) error { t.Fatal("daemon already running - must not start again"); return nil }

	if err := startOpkgTunAttach("opkgtun0", confPath); err != nil {
		t.Fatalf("startOpkgTunAttach: %v", err)
	}
	joined := joinCommands(commands)
	if strings.Contains(joined, "listen-port") {
		t.Fatalf("listen-port already correct - no enforcement needed; got:\n%s", joined)
	}
	if strings.Count(joined, "awg setconf opkgtun0") != 1 {
		t.Fatalf("expected exactly one setconf; got:\n%s", joined)
	}
	_ = os.Remove(filepath.Join(os.TempDir(), "xkeen-ui-opkgtun0.set"))
}

func TestStopOpkgTunAttach(t *testing.T) {
	setupOpkgTest(t)

	var commands [][]string
	procRunning := true
	opkgProcessRunningFn = func(string) bool { return procRunning }
	origPID := opkgDaemonPIDFn
	t.Cleanup(func() { opkgDaemonPIDFn = origPID })
	opkgDaemonPIDFn = func(string) (int, error) { return 4242, nil }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		commands = append(commands, append([]string{name}, args...))
		if name == "kill" {
			procRunning = false
		}
		return nil, nil
	}

	if err := stopOpkgTunAttach("opkgtun0"); err != nil {
		t.Fatalf("stopOpkgTunAttach: %v", err)
	}
	joined := joinCommands(commands)
	if !strings.Contains(joined, "kill 4242") {
		t.Fatalf("daemon must be killed; got:\n%s", joined)
	}
	if !strings.Contains(joined, "ip link set opkgtun0 down") {
		t.Fatalf("netdev must be set down; got:\n%s", joined)
	}
	if strings.Contains(joined, "ip link delete") || strings.Contains(joined, "ip link del") {
		// awg-quick down deletes the netdev; attach mode must NOT (NDMS owns it)
		t.Fatalf("attach mode must never delete the NDMS-owned netdev; got:\n%s", joined)
	}
}

func joinCommands(commands [][]string) string {
	var parts []string
	for _, c := range commands {
		parts = append(parts, strings.Join(c, " "))
	}
	return strings.Join(parts, "\n")
}

// ---------- Round-1 P0 fixes ----------

// The pgrep/pkill pattern must be end-anchored: without '$', checking for
// opkgtun0 also matches a daemon running as opkgtun01 (prefix collision),
// which makes stop kill the wrong daemon or Up hang on the socket wait.
func TestStopOpkgTunAttach_UsesKillByPID(t *testing.T) {
	setupOpkgTest(t)
	var kills []string
	procRunning := true
	opkgProcessRunningFn = func(string) bool { return procRunning }
	origPID := opkgDaemonPIDFn
	t.Cleanup(func() { opkgDaemonPIDFn = origPID })
	opkgDaemonPIDFn = func(string) (int, error) { return 4242, nil }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		if name == "kill" {
			kills = append(kills, strings.Join(args, " "))
			procRunning = false
		}
		return nil, nil
	}
	if err := stopOpkgTunAttach("opkgtun0"); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if len(kills) != 1 || kills[0] != "4242" {
		t.Fatalf("must kill daemon by PID (pkill unavailable on Keenetic), got %v", kills)
	}
}

// restartInterface (obfuscation apply) must use the attach path for OpkgTun
// netdevs: awg-quick would try to delete/recreate the NDMS-owned interface.
func TestRestartInterface_AttachMode(t *testing.T) {
	setupOpkgTest(t)
	h, _, awgDir := newTestAWGHandler(t)

	confPath := filepath.Join(awgDir, "opkgtun0.conf")
	conf := "[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\nJc = 8\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	procRunning := true
	opkgLinkExistsFn = func(string) bool { return true }
	opkgProcessRunningFn = func(string) bool { return procRunning }
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	origStart := opkgStartDaemonFn
	t.Cleanup(func() { opkgStartDaemonFn = origStart })
	opkgStartDaemonFn = func(_, _ string) error { procRunning = true; return nil }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "pkill":
			procRunning = false
		case name == "awg" && args[0] == "show":
			return []byte("interface: opkgtun0\n  listening port: 443\n"), nil
		}
		return nil, nil
	}
	_, iptRestore := captureIPTables()
	defer iptRestore()

	// awg-quick is not installed in the test env: success of the attach path
	// proves awg-quick was never invoked (it would fail and error out).
	if err := h.restartInterface("opkgtun0", subscription.AWGRoleServer, 0); err != nil {
		t.Fatalf("restart in attach mode failed: %v", err)
	}
}

// DownInterface must detect attach mode by the netdev (not the daemon) and
// must not panic when the config is missing from the store.
func TestDownInterface_AttachMode_DeadDaemon_NoPanic(t *testing.T) {
	setupOpkgTest(t)
	_, r, awgDir := newTestAWGHandler(t)

	conf := "[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"
	if err := os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	// Daemon dead, netdev exists, config NOT tracked in store.
	opkgLinkExistsFn = func(string) bool { return true }
	opkgProcessRunningFn = func(string) bool { return false }
	opkgExecFn = func(string, ...string) ([]byte, error) { return nil, nil }

	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"opkgtun0"}`)
	req := httptest.NewRequest(http.MethodPost, "/awg/down", body)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("down on attach iface with dead daemon must succeed, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OpkgTun attach mode") {
		t.Fatalf("attach-mode detach expected, got: %s", rec.Body.String())
	}
}

// DeleteConfig in attach mode must remove the server firewall rules and the
// runtime setconf file from the temp dir — not just kill the daemon.
func TestDeleteConfig_AttachMode_CleansUp(t *testing.T) {
	setupOpkgTest(t)
	_, r, awgDir := newTestAWGHandler(t)

	conf := "[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"
	if err := os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpSet := filepath.Join(os.TempDir(), "xkeen-ui-opkgtun0.set")
	os.WriteFile(tmpSet, []byte("x"), 0o600)
	defer os.Remove(tmpSet)

	procRunning := true
	opkgLinkExistsFn = func(string) bool { return true }
	opkgProcessRunningFn = func(string) bool { return procRunning }
	opkgExecFn = func(name string, _ ...string) ([]byte, error) {
		if name == "pkill" {
			procRunning = false
		}
		return nil, nil
	}
	calls, iptRestore := captureIPTables()
	defer iptRestore()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/awg/config/opkgtun0", http.NoBody)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", rec.Code, rec.Body.String())
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "-D INPUT -p udp --dport 443 -j ACCEPT") {
		t.Fatalf("firewall rules not removed on delete; calls:\n%s", joined)
	}
	if _, err := os.Stat(tmpSet); !os.IsNotExist(err) {
		t.Fatalf("runtime setconf file %s must be removed", tmpSet)
	}
}

// ---------- Round-2 fixes ----------

// If the NDMS netdev was deleted (Keenetic UI) while the daemon still runs,
// Down must still take the attach path — awg-quick down would fail forever
// on an interface it does not own.
func TestDownInterface_NetdevGone_DaemonAlive(t *testing.T) {
	setupOpkgTest(t)
	_, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"), []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n"), 0o600)

	opkgLinkExistsFn = func(string) bool { return false } // netdev deleted
	procRunning := true
	opkgProcessRunningFn = func(string) bool { return procRunning } // daemon alive
	origPID := opkgDaemonPIDFn
	t.Cleanup(func() { opkgDaemonPIDFn = origPID })
	opkgDaemonPIDFn = func(string) (int, error) { return 4242, nil }
	opkgExecFn = func(name string, _ ...string) ([]byte, error) {
		if name == "kill" {
			procRunning = false
		}
		return nil, nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/awg/down", strings.NewReader(`{"name":"opkgtun0"}`))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "OpkgTun attach mode") {
		t.Fatalf("attach path expected, got %d: %s", rec.Code, rec.Body.String())
	}
}

// listening port check must not prefix-match: port 443 must not be
// considered present when the daemon listens on 4430.
func TestStartOpkgTunAttach_PortPrefixMismatch(t *testing.T) {
	setupOpkgTest(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, "opkgtun0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nListenPort = 443\nJc = 8\n"), 0o600)

	var listenPortSet bool
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	opkgProcessRunningFn = func(string) bool { return true }
	opkgLinkExistsFn = func(string) bool { return true }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "awg" && args[0] == "show":
			return []byte("interface: opkgtun0\n  listening port: 4430\n"), nil
		case name == "awg" && args[0] == "set":
			listenPortSet = true
		}
		return nil, nil
	}

	if err := startOpkgTunAttach("opkgtun0", confPath); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	if !listenPortSet {
		t.Fatal("port 443 vs actual 4430: prefix match skipped listen-port enforcement")
	}
}

// restartInterface must re-apply the firewall even when the up step fails:
// the pre-down already removed the rules, leaving a production server open
// would be worse than reporting the restart error.
func TestRestartInterface_UpFailure_ReappliesFirewall(t *testing.T) {
	setupOpkgTest(t)
	h, _, awgDir := newTestAWGHandler(t)
	confPath := filepath.Join(awgDir, "opkgtun0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"), 0o600)

	procRunning := true
	opkgLinkExistsFn = func(string) bool { return true }
	opkgProcessRunningFn = func(string) bool { return procRunning }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "pkill":
			procRunning = false
		case name == "awg" && args[0] == "setconf":
			return nil, errors.New("simulated setconf failure") // up fails
		}
		return nil, nil
	}
	calls, iptRestore := captureIPTables()
	defer iptRestore()

	err := h.restartInterface("opkgtun0", subscription.AWGRoleServer, 0)
	if err == nil {
		t.Fatal("restart must report the up failure")
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "-I INPUT -p udp --dport 443 -j ACCEPT") {
		t.Fatalf("firewall must be re-applied after failed up; calls:\n%s", joined)
	}
}

// Down on an untracked server config must still remove the firewall rules
// (store reset / rescanned): symmetric with DeleteConfig.
func TestDownInterface_UntrackedServer_RemovesFirewall(t *testing.T) {
	setupOpkgTest(t)
	_, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"), []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"), 0o600)

	opkgLinkExistsFn = func(string) bool { return true }
	opkgProcessRunningFn = func(string) bool { return false }
	opkgExecFn = func(string, ...string) ([]byte, error) { return nil, nil }
	calls, iptRestore := captureIPTables()
	defer iptRestore()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/awg/down", strings.NewReader(`{"name":"opkgtun0"}`))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("down failed: %d %s", rec.Code, rec.Body.String())
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "-D INPUT -p udp --dport 443 -j ACCEPT") {
		t.Fatalf("firewall removal expected for untracked server; calls:\n%s", joined)
	}
}

// The runtime setconf file must not linger in /tmp after attach (it contains
// the server private key) and must be written symlink-safe.
func TestStartOpkgTunAttach_CleansRuntimeConf(t *testing.T) {
	setupOpkgTest(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, "opkgtun0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nListenPort = 443\nJc = 8\n"), 0o600)

	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	opkgProcessRunningFn = func(string) bool { return true }
	opkgLinkExistsFn = func(string) bool { return true }
	var setconfPath string
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		if name == "awg" && args[0] == "setconf" {
			setconfPath = args[len(args)-1]
		}
		if name == "awg" && args[0] == "show" {
			return []byte("interface: opkgtun0\n  listening port: 443\n"), nil
		}
		return nil, nil
	}

	if err := startOpkgTunAttach("opkgtun0", confPath); err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	if setconfPath == "" {
		t.Fatal("setconf was not called")
	}
	if _, err := os.Stat(setconfPath); !os.IsNotExist(err) {
		t.Fatalf("runtime conf %s must be removed after attach", setconfPath)
	}
}

// UpInterface failure branches: attach failure → 500 with context;
// awg-quick missing → distinct "install AWG tools" message.
func TestUpInterface_FailureBranches(t *testing.T) {
	setupOpkgTest(t)
	h, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"),
		[]byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"), 0o600)
	os.WriteFile(filepath.Join(awgDir, "plain.conf"),
		[]byte("[Interface]\nAddress = 10.9.0.1/24\nListenPort = 51820\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.9.0.2/32\n"), 0o600)

	// scan the temp dir into the store so role lookups succeed
	_, _ = h.store.ScanAWGConfigs(awgDir)

	opkgLinkExistsFn = func(string) bool { return true }
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	opkgProcessRunningFn = func(string) bool { return true }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		if name == "awg" && args[0] == "show" {
			return nil, errors.New("daemon dead")
		}
		return nil, nil
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/up", strings.NewReader(`{"name":"opkgtun0"}`)))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "OpkgTun attach failed") {
		t.Fatalf("attach failure must 500 with context, got %d: %s", rec.Code, rec.Body.String())
	}

	// quick-mode iface + awg-quick binary missing
	origQuick := awgQuickFn
	t.Cleanup(func() { awgQuickFn = origQuick })
	awgQuickFn = func(string, string) ([]byte, error) { return nil, exec.ErrNotFound }
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/awg/up", strings.NewReader(`{"name":"plain"}`)))
	if rec2.Code != http.StatusInternalServerError || !strings.Contains(rec2.Body.String(), "awg-quick binary not found") {
		t.Fatalf("missing binary must be a distinct error, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// generic quick failure echoes output
	awgQuickFn = func(string, string) ([]byte, error) { return []byte("boom"), errors.New("exit 1") }
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, httptest.NewRequest(http.MethodPost, "/awg/up", strings.NewReader(`{"name":"plain"}`)))
	if rec3.Code != http.StatusInternalServerError || !strings.Contains(rec3.Body.String(), "boom") {
		t.Fatalf("generic failure must echo output, got %d: %s", rec3.Code, rec3.Body.String())
	}
}

// Pins the documented semantics of the lifecycle engine's attach dispatch:
// an opkgtun-named config goes through attach mode regardless of tracked
// role — awg-quick must never touch an NDMS-owned netdev (even for a
// mis-classified client config).
func TestUpInterface_OpkgtunClientConfig_StillAttachMode(t *testing.T) {
	setupOpkgTest(t)
	h, r, awgDir := newTestAWGHandler(t)
	// client-shaped config in the opkgtun namespace (no ListenPort, has Endpoint)
	os.WriteFile(filepath.Join(awgDir, "opkgtun5.conf"),
		[]byte("[Interface]\nPrivateKey = x=\nAddress = 10.9.0.2/24\n\n[Peer]\nPublicKey = p=\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"), 0o600)
	_, _ = h.store.ScanAWGConfigs(awgDir)

	opkgLinkExistsFn = func(string) bool { return true } // NDMS netdev exists
	opkgProcessRunningFn = func(string) bool { return true }
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		if name == "awg" && args[0] == "show" {
			return []byte("interface: opkgtun5\n  listening port: 443\n"), nil
		}
		return nil, nil
	}
	quickUsed := false
	origQuick := awgQuickFn
	t.Cleanup(func() { awgQuickFn = origQuick })
	awgQuickFn = func(action, _ string) ([]byte, error) {
		if action == "up" {
			quickUsed = true
		}
		return nil, nil
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/up", strings.NewReader(`{"name":"opkgtun5"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("up failed: %d %s", rec.Code, rec.Body.String())
	}
	if quickUsed {
		t.Fatal("awg-quick must never run for opkgtun-owned netdevs, even for client-role configs")
	}
}

// removeClientFwmarkRouting with mark 0 (untracked/default) must be a no-op —
// never touch the main routing table or fwmark-0 rules.
func TestFwmarkRouting_MarkZeroIsNoop(t *testing.T) {
	h, _, _ := newTestAWGHandler(t)
	calls, restore := withIPRun(t)
	defer restore()
	h.applyClientFwmarkRouting("opkgtun0", 0)
	h.removeClientFwmarkRouting("opkgtun0", 0)
	if len(*calls) != 0 {
		t.Fatalf("mark 0 must not touch ip rules/routes, got %v", *calls)
	}
}

// ---------- cold attach: NDMS interface registered but netdev not up yet ----------

// Keenetic registers OpkgTun interfaces in its config but does not materialize
// the netdev until reboot. amneziawg-go creates the netdev itself on start,
// so attach mode must also apply when the netdev is absent but the NDMS config
// has the interface.
func TestIsOpkgTunAttachMode_NDMSRegisteredNoNetdev(t *testing.T) {
	setupOpkgTest(t)
	orig := opkgNDMSHasIfaceFn
	t.Cleanup(func() { opkgNDMSHasIfaceFn = orig })

	opkgLinkExistsFn = func(string) bool { return false }  // no netdev yet
	opkgNDMSHasIfaceFn = func(string) bool { return true } // but NDMS config has it

	if !isOpkgTunAttachMode("opkgtun1") {
		t.Fatal("NDMS-registered interface without netdev must be attach mode")
	}
	// A plain opkgtun-named config with no netdev and no NDMS entry is NOT
	// attach mode (avoids hijacking a quick-mode user config).
	opkgNDMSHasIfaceFn = func(string) bool { return false }
	if isOpkgTunAttachMode("opkgtun1") {
		t.Fatal("neither netdev nor NDMS interface must not be attach mode")
	}
}

// startOpkgTunAttach must wait for the netdev that amneziawg-go creates for a
// cold interface. If the daemon starts but the netdev never appears, it must
// fail rather than apply firewall rules to a nonexistent interface.
func TestStartOpkgTunAttach_ColdNetdevCreatedByDaemon(t *testing.T) {
	setupOpkgTest(t)
	dir := t.TempDir()
	confPath := filepath.Join(dir, "opkgtun1.conf")
	os.WriteFile(confPath, []byte("[Interface]\nAddress = 10.8.1.1/24\nListenPort = 4443\n"), 0o600)

	procRunning := false
	netdev := false
	opkgLinkExistsFn = func(string) bool { return netdev }
	opkgProcessRunningFn = func(string) bool { return procRunning }
	opkgLookPathFn = func(string) (string, error) { return "/opt/bin/amneziawg-go", nil }
	origStart := opkgStartDaemonFn
	t.Cleanup(func() { opkgStartDaemonFn = origStart })
	opkgStartDaemonFn = func(_, _ string) error {
		procRunning = true
		netdev = true // amneziawg-go created the netdev
		return nil
	}
	opkgExecFn = func(name string, args ...string) ([]byte, error) {
		if name == "awg" && args[0] == "show" {
			return []byte("interface: opkgtun1\n  listening port: 4443\n"), nil
		}
		return nil, nil
	}
	if err := startOpkgTunAttach("opkgtun1", confPath); err != nil {
		t.Fatalf("cold attach failed: %v", err)
	}
	if !netdev {
		t.Fatal("netdev must exist after daemon start")
	}
}
