package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// captureIPTables replaces runIPTablesFn and records every invocation.
// It emulates iptables -C semantics: -C succeeds only for rules that were
// previously added via -I/-A by the code under test.
func captureIPTables() (recorded *[]string, restore func()) {
	var mu sync.Mutex
	var calls []string
	added := map[string]bool{}
	orig := runIPTablesFn
	// ruleKey normalizes a call to its rule identity: "-t nat -A POSTROUTING x"
	// and "-t nat -C POSTROUTING x" map to the same key.
	ruleKey := func(args []string) string {
		var parts []string
		for _, a := range args {
			switch a {
			case "-I", "-A", "-C", "-D":
				continue
			default:
				parts = append(parts, a)
			}
		}
		return strings.Join(parts, " ")
	}
	runIPTablesFn = func(args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		key := ruleKey(args)
		action := ""
		for _, a := range args {
			if a == "-I" || a == "-A" || a == "-C" || a == "-D" {
				action = a
				break
			}
		}
		switch action {
		case "-C":
			return errRuleNotFound(added[key])
		case "-D":
			delete(added, key)
		default: // -I, -A
			added[key] = true
		}
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	return &calls, func() { runIPTablesFn = orig }
}

// errRuleNotFound converts the added-map lookup into the error contract of
// runIPTablesFn: nil means "rule exists", non-nil means "not found".
func errRuleNotFound(exists bool) error {
	if exists {
		return nil
	}
	return errors.New("rule not found")
}

func countExact(calls *[]string, rule string) int {
	n := 0
	for _, c := range *calls {
		if c == rule {
			n++
		}
	}
	return n
}

// ApplyFullTunnelFirewall must be idempotent: NDM fires the netfilter.d hook
// on every firewall rebuild (frequent), and RestoreAWGServerFirewalls calls
// Apply again for each active server. Without existence checks (-C before -I)
// duplicate INPUT/FORWARD rules accumulate on every event.
func TestApplyFullTunnelFirewall_Idempotent(t *testing.T) {
	calls, restore := captureIPTables()
	defer restore()

	p := AWGFirewallParams{
		Iface:    "opkgtun0",
		Port:     443,
		Subnet:   "10.8.0.0/24",
		LANIface: "br0",
		WANIface: "eth3",
	}

	ApplyFullTunnelFirewall(p)
	ApplyFullTunnelFirewall(p)

	dupes := []string{
		"-I INPUT -p udp --dport 443 -j ACCEPT",
		"-I INPUT -i opkgtun0 -p icmp -j ACCEPT",
		"-I INPUT -i opkgtun0 -p udp --dport 53 -j ACCEPT",
		"-I FORWARD -i opkgtun0 -o br0 -j ACCEPT",
		"-I FORWARD -i eth3 -o opkgtun0 -j ACCEPT",
		"-t nat -A POSTROUTING -s 10.8.0.0/24 -j MASQUERADE",
	}
	for _, rule := range dupes {
		if n := countExact(calls, rule); n != 1 {
			t.Fatalf("rule %q issued %d times after 2 applies, want 1", rule, n)
		}
	}
}

// ---------- coverage round: seams ----------

func withAWGShow(t *testing.T, out string, err error) {
	t.Helper()
	orig := awgShowFn
	t.Cleanup(func() { awgShowFn = orig })
	awgShowFn = func() ([]byte, error) { return []byte(out), err }
}

// getActiveInterfaces must tolerate every malformed `awg show` shape.
func TestGetActiveInterfaces_ParsingAndErrors(t *testing.T) {
	h, _, _ := newTestAWGHandler(t)

	// binary missing / exec error → empty map, no panic
	withAWGShow(t, "", errors.New("exec: not found"))
	if m := h.getActiveInterfaces(); len(m) != 0 {
		t.Fatalf("exec error must yield empty map, got %v", m)
	}

	// empty output → empty map
	withAWGShow(t, "", nil)
	if m := h.getActiveInterfaces(); len(m) != 0 {
		t.Fatalf("empty output must yield empty map, got %v", m)
	}

	// address before any interface: line → ignored
	withAWGShow(t, "  address: 10.0.0.1\n", nil)
	if m := h.getActiveInterfaces(); len(m) != 0 {
		t.Fatalf("orphan address line must be ignored, got %v", m)
	}

	// well-formed output
	withAWGShow(t, "interface: opkgtun0\n  public key: x\n  address: 10.8.0.1\ninterface: warp\n  address: 10.2.0.2\n", nil)
	m := h.getActiveInterfaces()
	if m["opkgtun0"] != "10.8.0.1" || m["warp"] != "10.2.0.2" {
		t.Fatalf("parse failed: %v", m)
	}
}

// The restore endpoint's singleflight: a second concurrent call must not
// re-run the iptables pass.
func TestRestoreFirewall_Singleflight(t *testing.T) {
	h, _, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"),
		[]byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"), 0o600)
	withAWGShow(t, "interface: opkgtun0\n  address: 10.8.0.1\n", nil)
	calls, iptRestore := captureIPTables()
	defer iptRestore()

	rec1 := httptest.NewRecorder()
	rec2 := httptest.NewRecorder()
	// Simulate in-flight restore: second call must take the 202 branch.
	restoreInFlight.Store(true)
	h.RestoreFirewall(rec2, httptest.NewRequest(http.MethodPost, "/x", http.NoBody))
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("in-flight restore must 202, got %d %s", rec2.Code, rec2.Body.String())
	}
	if len(*calls) != 0 {
		t.Fatalf("202 branch must not touch iptables, got %v", *calls)
	}
	restoreInFlight.Store(false)
	h.RestoreFirewall(rec1, httptest.NewRequest(http.MethodPost, "/x", http.NoBody))
	if rec1.Code != http.StatusOK || len(*calls) == 0 {
		t.Fatalf("free restore must run the pass, got %d, calls=%v", rec1.Code, *calls)
	}
}

// RestoreAWGServerFirewalls must only re-apply ACTIVE SERVER configs.
func TestRestoreAWGServerFirewalls_Filtering(t *testing.T) {
	h, _, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "opkgtun0.conf"),
		[]byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\n\n[Peer]\nPublicKey = pub=\nAllowedIPs = 10.8.0.2/32\n"), 0o600)
	// no active interfaces → nothing applied
	withAWGShow(t, "", nil)
	calls, iptRestore := captureIPTables()
	defer iptRestore()
	h.RestoreAWGServerFirewalls()
	if len(*calls) != 0 {
		t.Fatalf("inactive server must not be restored, got %v", *calls)
	}
	// active → rules applied
	withAWGShow(t, "interface: opkgtun0\n  address: 10.8.0.1\n", nil)
	h.RestoreAWGServerFirewalls()
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "-I INPUT -p udp --dport 443 -j ACCEPT") {
		t.Fatalf("active server must be restored; calls:\n%s", joined)
	}
}

// Apply with missing params must be a clean no-op.
func TestApplyFullTunnelFirewall_SkipOnMissingParams(t *testing.T) {
	calls, iptRestore := captureIPTables()
	defer iptRestore()
	ApplyFullTunnelFirewall(AWGFirewallParams{})
	if len(*calls) != 0 {
		t.Fatalf("empty params must skip, got %v", *calls)
	}
}

func withIPRun(t *testing.T) (calls *[]string, restore func()) {
	t.Helper()
	var recorded []string
	calls = &recorded
	orig := ipRunFn
	ipRunFn = func(args ...string) ([]byte, error) {
		recorded = append(recorded, "ip "+strings.Join(args, " "))
		return nil, nil
	}
	return calls, func() { ipRunFn = orig }
}

// applyClientFwmarkRouting must be check-before-add for both rule and route.
func TestApplyClientFwmarkRouting(t *testing.T) {
	h, _, _ := newTestAWGHandler(t)
	calls, restore := withIPRun(t)
	defer restore()

	h.applyClientFwmarkRouting("warp", 5)
	joined := strings.Join(*calls, "\n")
	for _, want := range []string{
		"ip rule show",
		"ip rule add fwmark 5 table 5 priority 1005",
		"ip route show table 5",
		"ip route add default dev warp table 5",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q; calls:\n%s", want, joined)
		}
	}
}

func TestRemoveClientFwmarkRouting(t *testing.T) {
	h, _, _ := newTestAWGHandler(t)
	calls, restore := withIPRun(t)
	defer restore()

	h.removeClientFwmarkRouting("warp", 5)
	joined := strings.Join(*calls, "\n")
	for _, want := range []string{
		"ip route del default dev warp table 5",
		"ip rule del fwmark 5",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q; calls:\n%s", want, joined)
		}
	}
}

// listenPortFromConf edge cases: missing, invalid, case-insensitive.
func TestListenPortFromConf(t *testing.T) {
	dir := t.TempDir()
	p := func(s string) string {
		f := filepath.Join(dir, "c.conf")
		os.WriteFile(f, []byte(s), 0o600)
		return f
	}
	if got := listenPortFromConf(p("[Interface]\nPrivateKey = x\n")); got != 0 {
		t.Fatalf("missing ListenPort must be 0, got %d", got)
	}
	if got := listenPortFromConf(p("[Interface]\nListenPort = notanumber\n")); got != 0 {
		t.Fatalf("invalid ListenPort must be 0, got %d", got)
	}
	if got := listenPortFromConf(p("[Interface]\nlistenport = 51820\n")); got != 51820 {
		t.Fatalf("case-insensitive fallback failed, got %d", got)
	}
	if got := listenPortFromConf(p("[Interface]\nListenPort = 443\n")); got != 443 {
		t.Fatalf("normal parse failed, got %d", got)
	}
}

// isServerConf fallback: untracked config classified by ListenPort.
func TestIsServerConf_Fallback(t *testing.T) {
	h, _, awgDir := newTestAWGHandler(t)
	server := filepath.Join(awgDir, "srv.conf")
	os.WriteFile(server, []byte("[Interface]\nListenPort = 443\n\n[Peer]\nPublicKey = p=\nAllowedIPs = 10.9.0.2/32\n"), 0o600)
	client := filepath.Join(awgDir, "cli.conf")
	os.WriteFile(client, []byte("[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = p=\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"), 0o600)
	if !h.isServerConf(server) {
		t.Fatal("untracked config with ListenPort must be server")
	}
	if h.isServerConf(client) {
		t.Fatal("untracked client (no ListenPort) must not be server")
	}
}

// The rules table must have no duplicates (double-insert after NDM rebuilds)
// and Remove must derive exactly one -D for every add rule.
func TestFullTunnelRules_NoDupsAndSymmetricRemove(t *testing.T) {
	p := normalizeFirewallParams(AWGFirewallParams{Iface: "opkgtun0", Subnet: "10.8.0.0/24", Port: 443, LANIface: "br0", WANIface: "eth3"})
	rules := fullTunnelRules(p)

	seen := map[string]bool{}
	for _, r := range rules {
		key := strings.Join(deriveDelete(r)[1:], " ")
		if seen[key] {
			t.Fatalf("duplicate rule identity: %v", r)
		}
		seen[key] = true
	}

	calls, iptRestore := captureIPTables()
	defer iptRestore()
	ApplyFullTunnelFirewall(p)
	RemoveFullTunnelFirewall(p)
	adds, dels := 0, 0
	for _, c := range *calls {
		fields := strings.Fields(c)
		isAdd := false
		for _, f := range fields {
			if f == "-I" || f == "-A" {
				isAdd = true
			}
			if f == "-D" {
				dels++
			}
		}
		if isAdd {
			adds++
		}
	}
	if adds != len(rules) {
		t.Fatalf("expected %d adds, got %d (calls: %v)", len(rules), adds, *calls)
	}
	if dels != len(rules) {
		t.Fatalf("every add must have exactly one delete: adds=%d dels=%d", adds, dels)
	}
}
