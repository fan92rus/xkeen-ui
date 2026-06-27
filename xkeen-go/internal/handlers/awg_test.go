package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/subscription"
)

// newTestAWGHandler creates an AWGHandler with a temp store and awgDir.
func newTestAWGHandler(t *testing.T) (*AWGHandler, *mux.Router, string) {
	t.Helper()
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "subscriptions.json")
	awgDir := filepath.Join(tmpDir, "awg")
	os.MkdirAll(awgDir, 0755)

	cfg := &config.Config{AWGConfigDir: awgDir}

	store, err := subscription.NewStore(storePath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	handler := NewAWGHandler(store, awgDir, cfg)
	r := mux.NewRouter()
	RegisterAWGRoutes(r, handler)

	return handler, r, awgDir
}

// ---------- ListInterfaces ----------

func TestAWGListInterfaces_Empty(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/awg/interfaces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp awgInterfacesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// interfaces must be [] not null — frontend expects an array
	if resp.Interfaces == nil {
		t.Fatal("interfaces is null — frontend will crash on .length")
	}
	if len(resp.Interfaces) != 0 {
		t.Errorf("expected 0 interfaces, got %d", len(resp.Interfaces))
	}
}

func TestAWGListInterfaces_WithConfigs(t *testing.T) {
	handler, router, awgDir := newTestAWGHandler(t)

	// Write two .conf files
	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`
	os.WriteFile(filepath.Join(awgDir, "warp1.conf"), []byte(confContent), 0644)
	os.WriteFile(filepath.Join(awgDir, "warp2.conf"), []byte(confContent), 0644)

	// Register configs in store
	handler.store.ScanAWGConfigs(awgDir)

	req := httptest.NewRequest(http.MethodGet, "/awg/interfaces", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp awgInterfacesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Interfaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(resp.Interfaces))
	}

	if resp.Interfaces[0].Name != "warp1" {
		t.Errorf("expected name 'warp1', got %q", resp.Interfaces[0].Name)
	}
	if resp.Interfaces[1].Name != "warp2" {
		t.Errorf("expected name 'warp2', got %q", resp.Interfaces[1].Name)
	}

	// Both should be inactive (no real awg tool)
	if resp.Interfaces[0].Active {
		t.Error("warp1 should be inactive")
	}
	if resp.Interfaces[1].Active {
		t.Error("warp2 should be inactive")
	}

	// Mark should be assigned sequentially
	if resp.Interfaces[0].Mark != 100 {
		t.Errorf("warp1 expected mark 100, got %d", resp.Interfaces[0].Mark)
	}
	if resp.Interfaces[1].Mark != 101 {
		t.Errorf("warp2 expected mark 101, got %d", resp.Interfaces[1].Mark)
	}
}

// ---------- validateAWGName ----------

func TestValidateAWGName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", false},
		{"simple", "warp", true},
		{"with_hyphen", "my-config", true},
		{"with_underscore", "my_config", true},
		{"alphanumeric", "warp123", true},
		{"with_dots", "config.v1", false},  // dots not allowed by validateAWGName
		{"path_traversal", "../etc", false},
		{"with_slash", "a/b", false},
		{"with_backslash", "a\\b", false},
		{"dot_prefix", ".hidden", false},
		{"dot_dot", "a..b", false},
		{"special_chars", "warp@1", false},
		{"with_space", "my config", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateAWGName(tt.input)
			if got != tt.want {
				t.Errorf("validateAWGName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------- UpInterface validation ----------

func TestAWGUpInterface_Validation(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	// Empty name should fail
	req := httptest.NewRequest(http.MethodPost, "/awg/up", body(`{"name":""}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}

	// Path traversal should fail
	req = httptest.NewRequest(http.MethodPost, "/awg/up", body(`{"name":"../etc/passwd"}`))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for path traversal, got %d", w.Code)
	}
}

func TestAWGDownInterface_Validation(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	// Empty name should fail
	req := httptest.NewRequest(http.MethodPost, "/awg/down", body(`{"name":""}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}
}

// ---------- DeleteConfig ----------

func TestAWGDeleteConfig_NotFound(t *testing.T) {
	_, router, _ := newTestAWGHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/awg/config/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent config, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAWGDeleteConfig_PathTraversal(t *testing.T) {
	// validateAWGName rejects traversal patterns
	tests := []struct {
		name  string
		input string
	}{
		{"contains_slash", "a/b"},
		{"contains_dotdot", ".."},
		{"starts_with_dot", ".hidden"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if validateAWGName(tt.input) {
				t.Errorf("validateAWGName(%q) should be false", tt.input)
			}
		})
	}
}

// ---------- Helpers ----------

func body(s string) io.Reader {
	return strings.NewReader(s)
}

// ---------- Obfuscation preset tests ----------

func TestGenerateRandomObfuscationParams(t *testing.T) {
	params := generateRandomObfuscationParams()

	// Must have all expected keys
	expectedKeys := []string{"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4", "I1"}
	for _, key := range expectedKeys {
		if _, ok := params[key]; !ok {
			t.Errorf("missing key %q in random params", key)
		}
	}

	// S3 and S4 must be 0 (no transport overhead)
	if params["S3"] != "0" {
		t.Errorf("S3 must be 0, got %s", params["S3"])
	}
	if params["S4"] != "0" {
		t.Errorf("S4 must be 0, got %s", params["S4"])
	}

	// H1-H4 must be non-zero (always keep active)
	for _, key := range []string{"H1", "H2", "H3", "H4"} {
		if params[key] == "0" {
			t.Errorf("%s must be non-zero in random preset", key)
		}
	}

	// Jmax must be > Jmin
	jmin, _ := strconv.Atoi(params["Jmin"])
	jmax, _ := strconv.Atoi(params["Jmax"])
	if jmax <= jmin {
		t.Errorf("Jmax (%d) must be > Jmin (%d)", jmax, jmin)
	}

	// Two calls should produce different results (uniqueness)
	params2 := generateRandomObfuscationParams()
	allSame := true
	for _, key := range expectedKeys {
		if params[key] != params2[key] {
			allSame = false
			break
		}
	}
	// Very unlikely all 12 params match by chance
	if allSame {
		t.Log("warning: two random generations produced identical params (extremely unlikely)")
	}
}

func TestObfuscationPresets_HaveAllParams(t *testing.T) {
	for _, preset := range getAWGObfuscationPresets() {
		if preset.Random {
			continue // random has no fixed params
		}
		// Non-plain presets must have all 12 params
		if preset.ID == "plain" {
			if len(preset.Params) != 0 {
				t.Errorf("plain preset should have no params, got %d", len(preset.Params))
			}
			continue
		}
		// Non-plain presets must have the 12 params we use (I2/I3 omitted = default)
		presetParamKeys := []string{"Jc", "Jmin", "Jmax", "S1", "S2", "S3", "S4", "H1", "H2", "H3", "H4", "I1"}
		for _, key := range presetParamKeys {
			if _, ok := preset.Params[key]; !ok {
				t.Errorf("preset %q missing param %q", preset.ID, key)
			}
		}
		// Non-plain presets must have non-zero H1-H4
		for _, key := range []string{"H1", "H2", "H3", "H4"} {
			if preset.Params[key] == "0" {
				t.Errorf("preset %q has H param = 0 (should always be non-zero)", preset.ID)
			}
		}
	}
}

func TestObfuscationPresets_S4AlwaysZero(t *testing.T) {
	for _, preset := range getAWGObfuscationPresets() {
		if preset.Random || preset.ID == "plain" {
			continue
		}
		if preset.Params["S4"] != "0" {
			t.Errorf("preset %q has S4 != 0 (S4 adds per-packet overhead)", preset.ID)
		}
	}
}

// ---------- readAWGParams ----------

func TestReadAWGParams(t *testing.T) {
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{
			Values: map[string]string{
				"Jc":         "1",
				"Jmin":       "20",
				"ListenPort": "443", // not an AWG param — must be excluded
				"PrivateKey": "xxx",  // not an AWG param — must be excluded
			},
		},
	}
	params := readAWGParams(conf)
	if params["Jc"] != "1" {
		t.Errorf("expected Jc=1, got %q", params["Jc"])
	}
	if params["Jmin"] != "20" {
		t.Errorf("expected Jmin=20, got %q", params["Jmin"])
	}
	if _, ok := params["ListenPort"]; ok {
		t.Error("ListenPort must not be treated as an AWG param")
	}
	if _, ok := params["PrivateKey"]; ok {
		t.Error("PrivateKey must not be treated as an AWG param")
	}
}

func TestReadAWGParams_PlainOrEmpty(t *testing.T) {
	// No AWG params set (plain WireGuard) → empty map.
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{
			Values: map[string]string{"PrivateKey": "xxx"},
		},
	}
	if got := readAWGParams(conf); len(got) != 0 {
		t.Errorf("expected empty params for plain config, got %v", got)
	}
	// nil interface → empty map, no panic.
	if got := readAWGParams(&subscription.AWGConf{}); len(got) != 0 {
		t.Errorf("expected empty params for nil interface, got %v", got)
	}
	// nil conf → empty map, no panic.
	if got := readAWGParams(nil); len(got) != 0 {
		t.Errorf("expected empty params for nil conf, got %v", got)
	}
}

// ---------- rewriteEndpointPort ----------

func TestRewriteEndpointPort(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		port   int
		expect string
	}{
		{
			name:   "ipv4 host",
			input:  "Endpoint = 146.120.53.90:443\n",
			port:   51820,
			expect: "Endpoint = 146.120.53.90:51820\n",
		},
		{
			name:   "domain host",
			input:  "Endpoint = vpn.example.com:443\n",
			port:   443,
			expect: "Endpoint = vpn.example.com:443\n",
		},
		{
			name:   "ipv6 bracketed host",
			input:  "Endpoint = [2001:db8::1]:443\n",
			port:   9999,
			expect: "Endpoint = [2001:db8::1]:9999\n",
		},
		{
			name:   "within full config — only Endpoint line changes",
			input:  "[Interface]\nPrivateKey = k\nAddress = 10.8.0.2/32\n\n[Peer]\nPublicKey = p\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n",
			port:   7443,
			expect: "[Interface]\nPrivateKey = k\nAddress = 10.8.0.2/32\n\n[Peer]\nPublicKey = p\nEndpoint = 1.2.3.4:7443\nAllowedIPs = 0.0.0.0/0\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.conf")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			if err := rewriteEndpointPort(path, tc.port); err != nil {
				t.Fatalf("rewriteEndpointPort failed: %v", err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != tc.expect {
				t.Errorf("expected:\n%s\ngot:\n%s", tc.expect, string(got))
			}
		})
	}
}

func TestRewriteEndpointPort_NoOpCases(t *testing.T) {
	// port <= 0 → no-op, file untouched.
	path := filepath.Join(t.TempDir(), "client.conf")
	orig := []byte("Endpoint = 1.2.3.4:443\n")
	os.WriteFile(path, orig, 0600)
	if err := rewriteEndpointPort(path, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(orig) {
		t.Errorf("port=0 should be a no-op, got %s", string(got))
	}
	// No Endpoint line → no-op.
	path2 := filepath.Join(t.TempDir(), "noep.conf")
	orig2 := []byte("[Interface]\nPrivateKey = k\n\n[Peer]\nPublicKey = p\n")
	os.WriteFile(path2, orig2, 0600)
	if err := rewriteEndpointPort(path2, 443); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got2, _ := os.ReadFile(path2)
	if string(got2) != string(orig2) {
		t.Errorf("file without Endpoint should be unchanged, got %s", string(got2))
	}
}

// ---------- syncClientWithServer (A+ fix) ----------

// writeTestConfigs writes a server.conf and a stored client config into the
// handler's awgDir and returns their paths.
func writeTestConfigs(t *testing.T, awgDir, serverConf, clientConf string) (serverPath, clientPath string) {
	t.Helper()
	serverPath = filepath.Join(awgDir, "server.conf")
	if err := os.WriteFile(serverPath, []byte(serverConf), 0600); err != nil {
		t.Fatal(err)
	}
	clientPath = filepath.Join(awgDir, "clients", "server-10.8.0.2.conf")
	os.MkdirAll(filepath.Dir(clientPath), 0755)
	if err := os.WriteFile(clientPath, []byte(clientConf), 0600); err != nil {
		t.Fatal(err)
	}
	return
}

// assertParam reads a config file and checks a key's value in [Interface].
func assertParam(t *testing.T, path, key, want string) {
	t.Helper()
	conf, err := subscription.ParseAWGConf(path)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if conf.Interface == nil {
		t.Fatal("no [Interface] section")
	}
	got := conf.Interface.Values[key]
	if got != want {
		t.Errorf("%s: expected %s=%q, got %q", filepath.Base(path), key, want, got)
	}
}

func TestSyncClientWithServer_AWGParams(t *testing.T) {
	// Regression: server has Jc=1 (manually edited), client snapshot has Jc=2
	// (stale from a prior Minimal preset). After sync the client must match the
	// server's current Jc=1 — AWG requires exact match for handshake.
	handler, _, awgDir := newTestAWGHandler(t)
	server := "[Interface]\nPrivateKey = skey\nListenPort = 443\nJc = 1\nJmin = 10\nJmax = 20\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n"
	client := "[Interface]\nPrivateKey = ckey\nAddress = 10.8.0.2/32\nJc = 2\nJmin = 20\nJmax = 40\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	_, clientPath := writeTestConfigs(t, awgDir, server, client)

	handler.syncClientWithServer("server", clientPath)

	assertParam(t, clientPath, "Jc", "1")
	assertParam(t, clientPath, "Jmin", "10")
	assertParam(t, clientPath, "Jmax", "20")
	// Client-owned fields must be preserved.
	assertParam(t, clientPath, "PrivateKey", "ckey")
	assertParam(t, clientPath, "Address", "10.8.0.2/32")
}

func TestSyncClientWithServer_PlainStripsClientParams(t *testing.T) {
	// Server is plain WireGuard (no AWG params). Syncing must REMOVE the params
	// from the stored client (a client with leftover AWG params cannot connect
	// to a plain server).
	handler, _, awgDir := newTestAWGHandler(t)
	server := "[Interface]\nPrivateKey = skey\nListenPort = 443\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n"
	client := "[Interface]\nPrivateKey = ckey\nAddress = 10.8.0.2/32\nJc = 8\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	_, clientPath := writeTestConfigs(t, awgDir, server, client)

	handler.syncClientWithServer("server", clientPath)

	conf, _ := subscription.ParseAWGConf(clientPath)
	for _, key := range []string{"Jc", "H1", "H2", "H3", "H4"} {
		if _, ok := conf.Interface.Values[key]; ok {
			t.Errorf("plain server: client still has AWG param %s after sync", key)
		}
	}
}

func TestSyncClientWithServer_EndpointPort(t *testing.T) {
	// Server ListenPort changed 443 → 51820; client Endpoint port must follow.
	handler, _, awgDir := newTestAWGHandler(t)
	server := "[Interface]\nPrivateKey = skey\nListenPort = 51820\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n"
	client := "[Interface]\nPrivateKey = ckey\nAddress = 10.8.0.2/32\n\n[Peer]\nPublicKey = spub\nEndpoint = 146.120.53.90:443\nAllowedIPs = 0.0.0.0/0\n"
	_, clientPath := writeTestConfigs(t, awgDir, server, client)

	handler.syncClientWithServer("server", clientPath)

	conf, _ := subscription.ParseAWGConf(clientPath)
	endpoint := ""
	if len(conf.Peers) > 0 {
		endpoint = conf.Peers[0].Values["Endpoint"]
	}
	if endpoint != "146.120.53.90:51820" {
		t.Errorf("expected endpoint port synced to 51820, got %q", endpoint)
	}
}

func TestSyncClientWithServer_PreservesEndpointHost(t *testing.T) {
	// The Endpoint HOST must be preserved (sync is file-only, no WAN detection).
	handler, _, awgDir := newTestAWGHandler(t)
	server := "[Interface]\nPrivateKey = skey\nListenPort = 443\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n"
	client := "[Interface]\nPrivateKey = ckey\nAddress = 10.8.0.2/32\n\n[Peer]\nPublicKey = spub\nEndpoint = vpn.example.com:443\nAllowedIPs = 0.0.0.0/0\n"
	_, clientPath := writeTestConfigs(t, awgDir, server, client)

	handler.syncClientWithServer("server", clientPath)

	conf, _ := subscription.ParseAWGConf(clientPath)
	endpoint := conf.Peers[0].Values["Endpoint"]
	if endpoint != "vpn.example.com:443" {
		t.Errorf("endpoint host must be preserved, got %q", endpoint)
	}
}

func TestSyncClientWithServer_MissingServerLeavesClientUntouched(t *testing.T) {
	// If server.conf is unreadable, the client config must be returned as-is.
	handler, _, awgDir := newTestAWGHandler(t)
	client := "[Interface]\nPrivateKey = ckey\nJc = 2\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	clientPath := filepath.Join(awgDir, "clients", "server-10.8.0.2.conf")
	os.MkdirAll(filepath.Dir(clientPath), 0755)
	os.WriteFile(clientPath, []byte(client), 0600)
	// No server.conf written.

	handler.syncClientWithServer("server", clientPath)

	got, _ := os.ReadFile(clientPath)
	if string(got) != client {
		t.Errorf("client config should be unchanged when server.conf is missing")
	}
}

func TestSyncClientWithServer_PreservesPeerBlock(t *testing.T) {
	// The entire [Peer] section (except Endpoint port) must survive intact.
	handler, _, awgDir := newTestAWGHandler(t)
	server := "[Interface]\nPrivateKey = skey\nListenPort = 443\nJc = 1\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n"
	client := "[Interface]\nPrivateKey = ckey\nAddress = 10.8.0.2/32\nJc = 2\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\n"
	_, clientPath := writeTestConfigs(t, awgDir, server, client)

	handler.syncClientWithServer("server", clientPath)

	conf, _ := subscription.ParseAWGConf(clientPath)
	if len(conf.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(conf.Peers))
	}
	p := conf.Peers[0]
	if p.Values["PublicKey"] != "spub" {
		t.Errorf("PublicKey not preserved: %q", p.Values["PublicKey"])
	}
	if p.Values["AllowedIPs"] != "0.0.0.0/0, ::/0" {
		t.Errorf("AllowedIPs not preserved: %q", p.Values["AllowedIPs"])
	}
	if p.Values["PersistentKeepalive"] != "25" {
		t.Errorf("PersistentKeepalive not preserved: %q", p.Values["PersistentKeepalive"])
	}
}

// ---------- GetPeerConfig end-to-end sync ----------

func TestGetPeerConfig_ReturnsSyncedConfig(t *testing.T) {
	// Full HTTP path: server edited to Jc=1 after peer creation; the stored
	// client snapshot is stale (Jc=2). GET /peer-config must return Jc=1.
	_, router, awgDir := newTestAWGHandler(t)
	server := "[Interface]\nPrivateKey = skey\nListenPort = 443\nJc = 1\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n"
	client := "[Interface]\nPrivateKey = ckey\nAddress = 10.8.0.2/32\nJc = 2\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	writeTestConfigs(t, awgDir, server, client)

	req := httptest.NewRequest(http.MethodGet, "/awg/peer-config/server?ip=10.8.0.2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success      bool   `json:"success"`
		ClientConfig string `json:"client_config"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Fatal("expected success=true")
	}
	if !strings.Contains(resp.ClientConfig, "Jc = 1") {
		t.Errorf("returned config should have synced Jc=1, got:\n%s", resp.ClientConfig)
	}
	if strings.Contains(resp.ClientConfig, "Jc = 2") {
		t.Errorf("returned config should NOT have stale Jc=2, got:\n%s", resp.ClientConfig)
	}
}

// ---------- extractPeers ----------

func TestExtractPeers_EmptyReturnsSliceNotNull(t *testing.T) {
	// Regression: a server config with no [Peer] sections must yield an empty
	// slice (JSON []) not a nil slice (JSON null), or the frontend crashes on
	// peers.length when opening a freshly-created server.
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{}},
	}
	got := extractPeers(conf)
	if got == nil {
		t.Fatal("extractPeers returned nil for a config with no peers — must be an empty slice so JSON serializes to [] not null")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 peers, got %d", len(got))
	}
}

func TestExtractPeers_FieldsAndIndex(t *testing.T) {
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{}},
		Peers: []*subscription.AWGConfigSection{
			{Type: "Peer", Comment: "peer: phone", Values: map[string]string{"PublicKey": "PUB1", "AllowedIPs": "10.8.0.2/32"}},
			{Type: "Peer", Values: map[string]string{"PublicKey": "PUB2", "AllowedIPs": "10.8.0.3/32, 10.8.0.4/32"}},
			{Type: "Peer", Comment: "peer: laptop", Values: map[string]string{"PublicKey": "PUB3"}}, // no AllowedIPs
		},
	}
	got := extractPeers(conf)
	if len(got) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(got))
	}
	// Peer 0: label from comment, IP from first AllowedIPs entry, index 0.
	if got[0].Label != "phone" {
		t.Errorf("peer0 label: want 'phone', got %q", got[0].Label)
	}
	if got[0].PublicKey != "PUB1" || got[0].IP != "10.8.0.2" || got[0].Index != 0 {
		t.Errorf("peer0 mismatch: %+v", got[0])
	}
	// Peer 1: no label, IP is the FIRST of multiple AllowedIPs, index 1.
	if got[1].Label != "" {
		t.Errorf("peer1 label should be empty, got %q", got[1].Label)
	}
	if got[1].IP != "10.8.0.3" {
		t.Errorf("peer1 IP should be first of list (10.8.0.3), got %q", got[1].IP)
	}
	if got[1].Index != 1 {
		t.Errorf("peer1 index: want 1, got %d", got[1].Index)
	}
	// Peer 2: no AllowedIPs → empty IP, index 2.
	if got[2].IP != "" {
		t.Errorf("peer2 IP should be empty (no AllowedIPs), got %q", got[2].IP)
	}
	if got[2].Index != 2 {
		t.Errorf("peer2 index: want 2, got %d", got[2].Index)
	}
}

func TestExtractPeers_AllowedIPsWithoutSlash(t *testing.T) {
	// AllowedIPs without a / prefix (bare IP) must not panic and must yield the IP.
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{}},
		Peers: []*subscription.AWGConfigSection{
			{Type: "Peer", Values: map[string]string{"AllowedIPs": "10.8.0.7"}},
		},
	}
	got := extractPeers(conf)
	if got[0].IP != "10.8.0.7" {
		t.Errorf("bare IP parse: want 10.8.0.7, got %q", got[0].IP)
	}
}

// ---------- allocatePeerIP ----------

func mustPeerSection(allowed string) *subscription.AWGConfigSection {
	return &subscription.AWGConfigSection{Type: "Peer", Values: map[string]string{"AllowedIPs": allowed}}
}

func TestAllocatePeerIP_DefaultSubnet(t *testing.T) {
	h := &AWGHandler{}
	// No peers, no subnet → default 10.8.0.0/24, lowest free = .2 (skips .0/.1/.255).
	conf := &subscription.AWGConf{Interface: &subscription.AWGConfigSection{Values: map[string]string{}}}
	ip, err := h.allocatePeerIP(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "10.8.0.2" {
		t.Errorf("want 10.8.0.2, got %s", ip)
	}
}

func TestAllocatePeerIP_LowestFreeWithGaps(t *testing.T) {
	h := &AWGHandler{}
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{}},
		Peers: []*subscription.AWGConfigSection{
			mustPeerSection("10.8.0.2/32"),
			mustPeerSection("10.8.0.4/32"), // gap at .3 → must pick .3, not .5
		},
	}
	ip, err := h.allocatePeerIP(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "10.8.0.3" {
		t.Errorf("want lowest free 10.8.0.3, got %s", ip)
	}
}

func TestAllocatePeerIP_CustomSubnet(t *testing.T) {
	h := &AWGHandler{}
	// Subnet derived from peers' AllowedIPs (192.168.5.x).
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{"Address": "192.168.5.1/24"}},
		Peers: []*subscription.AWGConfigSection{
			mustPeerSection("192.168.5.2/32"),
		},
	}
	ip, err := h.allocatePeerIP(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "192.168.5.3" {
		t.Errorf("want 192.168.5.3, got %s", ip)
	}
}

func TestAllocatePeerIP_ReservesServerAndBroadcast(t *testing.T) {
	h := &AWGHandler{}
	// Even if somehow .1/.255 appear in peers, allocator reserves them and must
	// never hand them out.
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{}},
		Peers: []*subscription.AWGConfigSection{
			mustPeerSection("10.8.0.1/32"), // server IP — must not be reused
		},
	}
	ip, err := h.allocatePeerIP(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == "10.8.0.1" {
		t.Errorf("must never allocate the server IP .1")
	}
	if ip != "10.8.0.2" {
		t.Errorf("want 10.8.0.2, got %s", ip)
	}
}

func TestAllocatePeerIP_SubnetExhausted(t *testing.T) {
	h := &AWGHandler{}
	// Fill .2 through .254 — next allocation must error.
	peers := make([]*subscription.AWGConfigSection, 0, 253)
	for i := 2; i <= 254; i++ {
		peers = append(peers, mustPeerSection(fmt.Sprintf("10.8.0.%d/32", i)))
	}
	conf := &subscription.AWGConf{
		Interface: &subscription.AWGConfigSection{Values: map[string]string{}},
		Peers: peers,
	}
	_, err := h.allocatePeerIP(conf)
	if err == nil {
		t.Fatal("expected error when subnet is exhausted, got nil")
	}
}

// ---------- buildPeerSection ----------

func TestBuildPeerSection_WithLabel(t *testing.T) {
	s := buildPeerSection("PUBKEY", "10.8.0.5", "my-phone")
	if !strings.Contains(s, "# peer: my-phone\n") {
		t.Errorf("missing label comment, got:\n%s", s)
	}
	if !strings.Contains(s, "[Peer]\n") {
		t.Errorf("missing [Peer] header, got:\n%s", s)
	}
	if !strings.Contains(s, "PublicKey = PUBKEY\n") {
		t.Errorf("missing PublicKey line, got:\n%s", s)
	}
	// IP must be written with /32 suffix regardless of input.
	if !strings.Contains(s, "AllowedIPs = 10.8.0.5/32\n") {
		t.Errorf("missing/bad AllowedIPs line, got:\n%s", s)
	}
}

func TestBuildPeerSection_NoLabel(t *testing.T) {
	s := buildPeerSection("PUB", "10.8.0.2", "")
	if strings.Contains(s, "# peer:") {
		t.Errorf("empty label must not emit a comment line, got:\n%s", s)
	}
	if !strings.Contains(s, "AllowedIPs = 10.8.0.2/32\n") {
		t.Errorf("missing AllowedIPs line, got:\n%s", s)
	}
}

// ---------- removePeerFromFile ----------

func writeServerConf(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.conf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const threePeerConf = `[Interface]
PrivateKey = skey
Jc = 1

# peer: phone
[Peer]
PublicKey = PUB1
AllowedIPs = 10.8.0.2/32

# peer: laptop
[Peer]
PublicKey = PUB2
AllowedIPs = 10.8.0.3/32

[Peer]
PublicKey = PUB3
AllowedIPs = 10.8.0.4/32
`

func TestRemovePeerFromFile_ByPublicKey(t *testing.T) {
	path := writeServerConf(t, threePeerConf)
	if err := removePeerFromFile(path, "PUB2", "", -1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "PUB2") {
		t.Errorf("PUB2 peer should have been removed")
	}
	if !strings.Contains(string(got), "PUB1") || !strings.Contains(string(got), "PUB3") {
		t.Errorf("other peers must survive, got:\n%s", string(got))
	}
	// The label comment of the removed peer must also be stripped.
	if strings.Contains(string(got), "peer: laptop") {
		t.Errorf("removed peer's label comment should be stripped, got:\n%s", string(got))
	}
	if !strings.Contains(string(got), "peer: phone") {
		t.Errorf("surviving peer's label comment must remain, got:\n%s", string(got))
	}
}

func TestRemovePeerFromFile_ByIP(t *testing.T) {
	path := writeServerConf(t, threePeerConf)
	if err := removePeerFromFile(path, "", "10.8.0.4", -1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "PUB3") || strings.Contains(string(got), "10.8.0.4") {
		t.Errorf("peer with IP 10.8.0.4 should have been removed, got:\n%s", string(got))
	}
}

func TestRemovePeerFromFile_ByIndex(t *testing.T) {
	// Index fallback (used when key+ip are empty, e.g. on the user's router).
	path := writeServerConf(t, threePeerConf)
	if err := removePeerFromFile(path, "", "", 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "PUB2") {
		t.Errorf("peer at index 1 (PUB2) should have been removed")
	}
	if !strings.Contains(string(got), "PUB1") || !strings.Contains(string(got), "PUB3") {
		t.Errorf("peers at index 0 and 2 must survive, got:\n%s", string(got))
	}
}

func TestRemovePeerFromFile_NoMatchLeavesUntouched(t *testing.T) {
	path := writeServerConf(t, threePeerConf)
	orig := threePeerConf
	if err := removePeerFromFile(path, "NONEXISTENT", "", -1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := os.ReadFile(path)
	// All three peers must still be present.
	for _, key := range []string{"PUB1", "PUB2", "PUB3"} {
		if !strings.Contains(string(got), key) {
			t.Errorf("%s should remain when no peer matches, got:\n%s", key, string(got))
		}
	}
	_ = orig
}

// ---------- maskKey ----------

func TestMaskKey(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "***"},         // empty
		{"abcd", "***"},      // short (≤8)
		{"abcdefgh", "***"},  // exactly 8
		{"abcdefghi", "abcd...fghi"},  // 9 → first4...last4
		{"0123456789abcdef", "0123...cdef"}, // 16
	}
	for _, tc := range cases {
		if got := maskKey(tc.in); got != tc.want {
			t.Errorf("maskKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ---------- clientConfigPath ----------

func TestClientConfigPath(t *testing.T) {
	h := &AWGHandler{awgDir: "/opt/etc/awg"}
	// Bare IP (no slash) — kept as-is.
	p := h.clientConfigPath("server", "10.8.0.2")
	want := "/opt/etc/awg/clients/server-10.8.0.2.conf"
	// Normalize separators for cross-platform comparison.
	if filepath.ToSlash(p) != want {
		t.Errorf("bare IP: want %s, got %s", want, filepath.ToSlash(p))
	}
	// IP with /32 — slash replaced by underscore so it's a valid filename.
	p2 := h.clientConfigPath("server", "10.8.0.2/32")
	want2 := "/opt/etc/awg/clients/server-10.8.0.2_32.conf"
	if filepath.ToSlash(p2) != want2 {
		t.Errorf("CIDR IP: want %s, got %s", want2, filepath.ToSlash(p2))
	}
}

// ---------- awgParamsEqual ----------

func TestAWGParamsEqual(t *testing.T) {
	if !awgParamsEqual(map[string]string{}, map[string]string{}) {
		t.Error("two empty maps should be equal")
	}
	if !awgParamsEqual(map[string]string{"Jc": "1", "H1": "2"}, map[string]string{"H1": "2", "Jc": "1"}) {
		t.Error("maps with same keys/values in different order should be equal")
	}
	if awgParamsEqual(map[string]string{"Jc": "1"}, map[string]string{"Jc": "1", "H1": "2"}) {
		t.Error("maps of different length should NOT be equal")
	}
	if awgParamsEqual(map[string]string{"Jc": "1"}, map[string]string{"Jc": "2"}) {
		t.Error("maps with differing values should NOT be equal")
	}
	if awgParamsEqual(map[string]string{"Jc": "1"}, map[string]string{"H1": "1"}) {
		t.Error("maps with differing keys should NOT be equal")
	}
}

// ---------- detectObfuscationPreset ----------

func TestDetectObfuscationPreset(t *testing.T) {
	cases := []struct {
		name string
		conf string
		want string
	}{
		{
			name: "plain (no AWG params)",
			conf: "[Interface]\nPrivateKey = k\nListenPort = 443\n\n[Peer]\nPublicKey = p\nAllowedIPs = 10.8.0.2/32\n",
			want: "plain",
		},
		{
			name: "minimal preset",
			conf: "[Interface]\nPrivateKey = k\nJc = 2\nJmin = 20\nJmax = 40\nS1 = 0\nS2 = 0\nS3 = 0\nS4 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\nI1 = 0\n",
			want: "minimal",
		},
		{
			name: "full preset",
			conf: "[Interface]\nPrivateKey = k\nJc = 8\nJmin = 50\nJmax = 100\nS1 = 30\nS2 = 20\nS3 = 0\nS4 = 0\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\nI1 = 0\n",
			want: "full",
		},
		{
			name: "custom (non-preset values)",
			conf: "[Interface]\nPrivateKey = k\nJc = 1\nH1 = 1\nH2 = 2\nH3 = 3\nH4 = 4\n",
			want: "custom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeServerConf(t, tc.conf)
			got, err := detectObfuscationPreset(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestDetectObfuscationPreset_MissingFile(t *testing.T) {
	_, err := detectObfuscationPreset(filepath.Join(t.TempDir(), "nope.conf"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// ---------- updateClientConfigsObfuscation ----------

func TestUpdateClientConfigsObfuscation(t *testing.T) {
	_, _, awgDir := newTestAWGHandler(t)
	h := &AWGHandler{awgDir: awgDir}
	clientsDir := filepath.Join(awgDir, "clients")
	os.MkdirAll(clientsDir, 0755)

	// Two client configs for "server", one unrelated file (different server name).
	serverClient := "[Interface]\nPrivateKey = c1\nJc = 1\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	serverClient2 := "[Interface]\nPrivateKey = c2\nJc = 1\n\n[Peer]\nPublicKey = spub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	unrelated := "[Interface]\nPrivateKey = cu\nJc = 1\n\n[Peer]\nPublicKey = upub\nEndpoint = 1.2.3.4:443\nAllowedIPs = 0.0.0.0/0\n"
	os.WriteFile(filepath.Join(clientsDir, "server-10.8.0.2.conf"), []byte(serverClient), 0600)
	os.WriteFile(filepath.Join(clientsDir, "server-10.8.0.3.conf"), []byte(serverClient2), 0600)
	os.WriteFile(filepath.Join(clientsDir, "other-10.0.0.2.conf"), []byte(unrelated), 0600)

	params := map[string]string{"Jc": "5", "H1": "1", "H2": "2", "H3": "3", "H4": "4"}
	h.updateClientConfigsObfuscation("server", params)

	// Both server clients updated.
	assertParam(t, filepath.Join(clientsDir, "server-10.8.0.2.conf"), "Jc", "5")
	assertParam(t, filepath.Join(clientsDir, "server-10.8.0.3.conf"), "Jc", "5")
	// Unrelated client (different prefix) untouched.
	assertParam(t, filepath.Join(clientsDir, "other-10.0.0.2.conf"), "Jc", "1")
}

func TestUpdateClientConfigsObfuscation_NoClientsDir(t *testing.T) {
	// Missing clients/ directory must be a silent no-op, not a crash.
	h := &AWGHandler{awgDir: t.TempDir()}
	h.updateClientConfigsObfuscation("server", map[string]string{"Jc": "5"})
	// Reaching here without panic is the pass condition.
}
