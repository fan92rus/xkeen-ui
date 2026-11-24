package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withPeersExec(t *testing.T) {
	t.Helper()
	origExec, origIn := peersExecFn, peersExecFnIn
	t.Cleanup(func() { peersExecFn, peersExecFnIn = origExec, origIn })
	peersExecFn = func(name string, args ...string) ([]byte, error) {
		switch {
		case name == "awg" && args[0] == "genkey":
			return []byte("PRIVKEY123\n"), nil
		case name == "awg-quick" && args[0] == "strip":
			return []byte("[Interface]\nListenPort = 443\n"), nil
		case name == "awg" && args[0] == "syncconf":
			return []byte("ok\n"), nil
		case name == "ip" && args[0] == "route":
			return []byte("8.8.8.8 via 1.2.3.1 dev eth3 src 146.120.53.90\n"), nil
		}
		return nil, nil
	}
	peersExecFnIn = func(name string, args []string, stdin string) ([]byte, error) {
		_ = stdin
		if name == "awg" && args[0] == "pubkey" {
			return []byte("PUBKEY456\n"), nil
		}
		return nil, nil
	}
}

// AddPeer e2e: keypair generation, peer appended to the server config, IP
// allocated from the server subnet, client config saved with server pubkey
// and endpoint, running interface synced.
func TestAddPeer_EndToEnd(t *testing.T) {
	withPeersExec(t)
	h, r, awgDir := newTestAWGHandler(t)

	confPath := filepath.Join(awgDir, "opkgtun0.conf")
	os.WriteFile(confPath, []byte("[Interface]\nAddress = 10.8.0.1/24\nListenPort = 443\nPrivateKey = SERVERPRIV=\nJc = 8\n\n[Peer]\nPublicKey = EXISTING=\nAllowedIPs = 10.8.0.2/32\n"), 0o600)
	_, _ = h.store.ScanAWGConfigs(awgDir)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/awg/peers/opkgtun0", strings.NewReader(`{"label":"phone"}`))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("AddPeer failed: %d %s", rec.Code, rec.Body.String())
	}

	// peer appended with allocated IP 10.8.0.3 and generated pubkey
	srv, _ := os.ReadFile(confPath)
	if !strings.Contains(string(srv), "PUBKEY456") || !strings.Contains(string(srv), "10.8.0.3/32") {
		t.Fatalf("peer not appended correctly:\n%s", srv)
	}
	// client config saved with server pub and endpoint
	c, err := os.ReadFile(filepath.Join(awgDir, "clients", "opkgtun0-10.8.0.3.conf"))
	if err != nil {
		t.Fatalf("client config not saved: %v", err)
	}
	if !strings.Contains(string(c), "PUBKEY456") || !strings.Contains(string(c), "146.120.53.90") {
		t.Fatalf("client config missing server pubkey/endpoint:\n%s", c)
	}
}

// DownInterface success path for a quick-mode client: fwmark routing removed,
// awg-quick down invoked, interface gone from `awg show`.
func TestDownInterface_ClientSuccess(t *testing.T) {
	h, r, awgDir := newTestAWGHandler(t)
	os.WriteFile(filepath.Join(awgDir, "warp.conf"),
		[]byte("[Interface]\nPrivateKey = x=\nAddress = 10.2.0.2/24\n\n[Peer]\nPublicKey = p=\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"), 0o600)
	_, _ = h.store.ScanAWGConfigs(awgDir)

	downCalls := 0
	origQuick := awgQuickFn
	t.Cleanup(func() { awgQuickFn = origQuick })
	awgQuickFn = func(action, path string) ([]byte, error) {
		if action == "down" && strings.HasSuffix(path, "warp.conf") {
			downCalls++
		}
		return nil, nil
	}
	withAWGShow(t, "", nil) // after down: no active interfaces
	ipCalls, ipRestore := withIPRun(t)
	defer ipRestore()

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/awg/down", strings.NewReader(`{"name":"warp"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("down failed: %d %s", rec.Code, rec.Body.String())
	}
	if downCalls != 1 {
		t.Fatalf("awg-quick down must run once, got %d", downCalls)
	}
	if !strings.Contains(strings.Join(*ipCalls, "\n"), "ip rule del fwmark") {
		t.Fatalf("client fwmark teardown missing: %v", *ipCalls)
	}
}
