package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeExecutor records executed commands for asserting apply behavior.
type fakeExecutor struct {
	mu    sync.Mutex
	execs []string
}

func (f *fakeExecutor) Execute(_ context.Context, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, name+" "+strings.Join(args, " "))
	return "", nil
}

func (f *fakeExecutor) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.execs))
	copy(out, f.execs)
	return out
}

// setupProxyPortsTest creates a temp dir structure and a ProxyPortsHandler.
func setupProxyPortsTest(t *testing.T) (h *ProxyPortsHandler, exec *fakeExecutor, xkeenDir, xrayDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	xkeenDir = filepath.Join(tmpDir, "xkeen")
	xrayDir = filepath.Join(tmpDir, "xray")
	backupDir := filepath.Join(tmpDir, "backups")
	for _, d := range []string{xkeenDir, xrayDir, backupDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	exec = &fakeExecutor{}
	h = NewProxyPortsHandler(xkeenDir, xrayDir, backupDir, exec)
	return h, exec, xkeenDir, xrayDir
}

// writeRouting writes a minimal 05_routing.json with the given rules.
func writeRouting(t *testing.T, xrayDir string, rules []map[string]interface{}) {
	t.Helper()
	doc := map[string]interface{}{
		"routing": map[string]interface{}{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal routing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(xrayDir, "05_routing.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write routing: %v", err)
	}
}

func sampleRules() []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "field", "name": "Ru Direct", "domain": []string{"regexp:.*\\.ru$"}, "outboundTag": "direct"},
		{"type": "field", "network": "udp", "outboundTag": "direct"},
		{"network": "tcp,udp", "balancerTag": "default-balancer"},
	}
}

// ---------------------------------------------------------------------------
// ValidatePortList
// ---------------------------------------------------------------------------

func TestValidatePortList(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty", "", "", false},
		{"whitespace", "  ", "", false},
		{"single", "443", "443", false},
		{"multiple", "80,443", "80,443", false},
		{"range colon", "50000:51000", "50000:51000", false},
		{"range dash", "50000-51000", "50000:51000", false},
		{"mixed", " 8443 , 50000:51000, 80 ", "80,8443,50000:51000", false},
		{"dupes removed", "443,443,80", "80,443", false},
		{"sorted", "8443,80,50000:51000", "80,8443,50000:51000", false},
		{"zero allowed", "0", "0", false},
		{"too high", "65536", "", true},
		{"negative", "-1", "", true},
		{"bad token", "abc", "", true},
		{"reversed range", "51000:50000", "", true},
		{"range overflow", "1:70000", "", true},
		{"too long", strings.Repeat("443,", 200), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ValidatePortList(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ValidatePortList(%q) expected error, got %q", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidatePortList(%q) unexpected error: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("ValidatePortList(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings/proxy-ports
// ---------------------------------------------------------------------------

func TestGetProxyPorts_NoFiles(t *testing.T) {
	h, _, _, _ := setupProxyPortsTest(t)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/proxy-ports", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetProxyPorts(rr, req)

	var resp ProxyPortsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Mode != "none" || resp.Ports != "" {
		t.Fatalf("expected mode=none, got mode=%q ports=%q", resp.Mode, resp.Ports)
	}
}

func TestGetProxyPorts_Proxying(t *testing.T) {
	h, _, xkeenDir, _ := setupProxyPortsTest(t)
	if err := os.WriteFile(filepath.Join(xkeenDir, "port_proxying.lst"), []byte("80,443,50000:51000\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/proxy-ports", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetProxyPorts(rr, req)

	var resp ProxyPortsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Mode != "proxying" || resp.Ports != "80,443,50000:51000" {
		t.Fatalf("unexpected GET result: %+v", resp)
	}
}

func TestGetProxyPorts_ExcludePrecedence(t *testing.T) {
	h, _, xkeenDir, _ := setupProxyPortsTest(t)
	// Both files present: proxying wins (xkeen precedence).
	if err := os.WriteFile(filepath.Join(xkeenDir, "port_proxying.lst"), []byte("80,443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xkeenDir, "port_exclude.lst"), []byte("6881\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/proxy-ports", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetProxyPorts(rr, req)

	var resp ProxyPortsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Mode != "proxying" || resp.Ports != "80,443" {
		t.Fatalf("expected proxying to take precedence, got %+v", resp)
	}
}

func TestGetProxyPorts_CommentsStripped(t *testing.T) {
	h, _, xkeenDir, _ := setupProxyPortsTest(t)
	content := "# проксируем только веб\n80,443  # web\n"
	if err := os.WriteFile(filepath.Join(xkeenDir, "port_proxying.lst"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/proxy-ports", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetProxyPorts(rr, req)

	var resp ProxyPortsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Ports != "80,443" {
		t.Fatalf("comments must be stripped, got %q", resp.Ports)
	}
}

// ---------------------------------------------------------------------------
// PUT /api/settings/proxy-ports
// ---------------------------------------------------------------------------

func TestUpdateProxyPorts_Proxying(t *testing.T) {
	h, exec, xkeenDir, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())

	body := `{"mode":"proxying","ports":"80,443,50000:51000","udp_ports":"50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var resp ProxyPortsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Ok || resp.Mode != "proxying" || resp.Ports != "80,443,50000:51000" || resp.UDPPorts != "50000:51000" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// .lst file written correctly, in xkeen's format (one port/range per
	// line, ':' ranges — a comma-joined single line would be dropped by
	// xkeen's read_ports_file entirely).
	mode, ports := h.readActivePorts()
	if mode != "proxying" || ports != "80,443,50000:51000" {
		t.Fatalf("list file not written correctly: mode=%q ports=%q", mode, ports)
	}
	raw, err := os.ReadFile(filepath.Join(xkeenDir, "port_proxying.lst"))
	if err != nil {
		t.Fatalf("read proxying list: %v", err)
	}
	if string(raw) != "80\n443\n50000:51000\n" {
		t.Fatalf("proxying list must be one port per line, got %q", string(raw))
	}

	// Routing: managed UDP rule inserted before the udp->direct catch-all.
	doc, ok := h.readRoutingDoc()
	if !ok {
		t.Fatal("routing file missing")
	}
	rules := routingRules(doc)
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules, got %d: %+v", len(rules), rules)
	}
	managed := rules[1]
	if name, _ := managed["name"].(string); name != ManagedUDPRuleName {
		t.Fatalf("managed rule not at index 1: %+v", rules)
	}
	if net, _ := managed["network"].(string); net != "udp" {
		t.Fatalf("managed rule network = %q", net)
	}
	if bt, _ := managed["balancerTag"].(string); bt != "default-balancer" {
		t.Fatalf("managed rule balancerTag = %q", bt)
	}
	if port, _ := managed["port"].(string); port != "50000-51000" {
		t.Fatalf("managed rule port must be a string in Xray syntax (\"50000-51000\"), got %#v", managed["port"])
	}
	if rules[2]["outboundTag"] != "direct" {
		t.Fatalf("udp->direct catch-all must follow the managed rule: %+v", rules[2])
	}

	// Apply triggered via xkeen -restart (the xray pre-flight check also
	// records a call and is filtered out).
	var xkeenCalls []string
	for _, c := range exec.calls() {
		if strings.HasPrefix(c, "xkeen") {
			xkeenCalls = append(xkeenCalls, c)
		}
	}
	if len(xkeenCalls) != 1 || xkeenCalls[0] != "xkeen -restart" {
		t.Fatalf("expected exactly one `xkeen -restart`, got %v", exec.calls())
	}
}

func TestUpdateProxyPorts_ManagedRuleXrayPortFormat(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())

	// Mix of single ports and ranges (":" is the editor's canonical form).
	body := `{"mode":"proxying","ports":"80,443,50000:51000","udp_ports":"443,50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	doc, _ := h.readRoutingDoc()
	for _, r := range routingRules(doc) {
		if name, _ := r["name"].(string); name != ManagedUDPRuleName {
			continue
		}
		port, ok := r["port"].(string)
		if !ok {
			t.Fatalf("managed rule port must be a JSON string, got %#v", r["port"])
		}
		if port != "443,50000-51000" {
			t.Fatalf("managed rule port = %q, want \"443,50000-51000\"", port)
		}
		return
	}
	t.Fatal("managed rule not found")
}

func TestUpdateProxyPorts_UDPPortsReadBackNormalized(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())

	body := `{"mode":"proxying","ports":"80,443,50000:51000","udp_ports":"50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// GET must report the UDP ports in the canonical ":"-form the editor uses.
	req = httptest.NewRequest(http.MethodGet, "/api/settings/proxy-ports", http.NoBody)
	rr = httptest.NewRecorder()
	h.GetProxyPorts(rr, req)
	var resp ProxyPortsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.UDPPorts != "50000:51000" {
		t.Fatalf("UDPPorts = %q, want \"50000:51000\"", resp.UDPPorts)
	}
}

func TestManagedUDPRulePorts_StringForm(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	rules := sampleRules()
	rules = append(rules, map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        "50000-51000,443",
		"balancerTag": "default-balancer",
	})
	writeRouting(t, xrayDir, rules)

	ports, ok := h.managedUDPRulePorts()
	if !ok || ports != "443,50000:51000" {
		t.Fatalf("string form: got ports=%q ok=%v, want \"443,50000:51000\"", ports, ok)
	}
}

func TestManagedUDPRulePorts_LegacyArrayForm(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	rules := sampleRules()
	rules = append(rules, map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        []interface{}{"50000:51000", "443"},
		"balancerTag": "default-balancer",
	})
	writeRouting(t, xrayDir, rules)

	ports, ok := h.managedUDPRulePorts()
	if !ok || ports != "443,50000:51000" {
		t.Fatalf("legacy array form: got ports=%q ok=%v, want \"443,50000:51000\"", ports, ok)
	}
}

func TestManagedUDPRulePorts_SingleNumber(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	rules := sampleRules()
	rules = append(rules, map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        443,
		"balancerTag": "default-balancer",
	})
	writeRouting(t, xrayDir, rules)

	ports, ok := h.managedUDPRulePorts()
	if !ok || ports != "443" {
		t.Fatalf("number form: got ports=%q ok=%v, want \"443\"", ports, ok)
	}
}

func TestManagedUDPRulePorts_MissingRule(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())

	if _, ok := h.managedUDPRulePorts(); ok {
		t.Fatal("expected ok=false when the managed rule is absent")
	}
}

func TestUpdateProxyPorts_IndependentUDP(t *testing.T) {
	h, exec, _, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())

	// UDP ports may differ from the TCP list: only TCP 80,443 proxied, but
	// UDP 50000:51000 (e.g. Discord voice) proxied too.
	body := `{"mode":"proxying","ports":"80,443","udp_ports":"50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}

	// TCP list (port_proxying.lst) must NOT contain the UDP-only ports.
	data, err := os.ReadFile(filepath.Join(h.xkeenConfigDir, proxyPortsFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "80\n443\n" {
		t.Fatalf("port_proxying.lst = %q, want %q", got, "80\n443\n")
	}

	// Managed UDP rule must carry the independent UDP range.
	if ports, ok := h.managedUDPRulePorts(); !ok || ports != "50000:51000" {
		t.Fatalf("managed UDP rule ports = %q (ok=%v), want %q", ports, ok, "50000:51000")
	}

	// Round-trip: GET reports the independent lists.
	get := httptest.NewRequest(http.MethodGet, "/api/settings/proxy-ports", http.NoBody)
	grr := httptest.NewRecorder()
	h.GetProxyPorts(grr, get)
	var resp ProxyPortsResponse
	if err := json.Unmarshal(grr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Ports != "80,443" || resp.UDPPorts != "50000:51000" {
		t.Fatalf("GET = ports %q udp %q, want %q / %q", resp.Ports, resp.UDPPorts, "80,443", "50000:51000")
	}
	xkeenCalls := []string{}
	for _, c := range exec.calls() {
		if strings.HasPrefix(c, "xkeen") {
			xkeenCalls = append(xkeenCalls, c)
		}
	}
	if len(xkeenCalls) != 1 || xkeenCalls[0] != "xkeen -restart" {
		t.Fatalf("expected a single xkeen -restart, got %v", exec.calls())
	}
}

func TestUpdateProxyPorts_InvalidMode(t *testing.T) {
	h, _, _, _ := setupProxyPortsTest(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"bogus","ports":"80"}`))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestUpdateProxyPorts_NoneClearsFiles(t *testing.T) {
	h, exec, _, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())

	// First enable proxying list.
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"proxying","ports":"80,443","udp_ports":""}`))
	h.UpdateProxyPorts(httptest.NewRecorder(), req)

	// Then switch to "none" (proxy all ports): both files cleared, rule removed.
	req = httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"none","ports":"","udp_ports":""}`))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	mode, ports := h.readActivePorts()
	if mode != "none" || ports != "" {
		t.Fatalf("expected mode=none, got mode=%q ports=%q", mode, ports)
	}

	doc, _ := h.readRoutingDoc()
	rules := routingRules(doc)
	if len(rules) != len(sampleRules()) {
		t.Fatalf("managed rule not removed: %+v", rules)
	}
	for _, r := range rules {
		if name, _ := r["name"].(string); name == ManagedUDPRuleName {
			t.Fatalf("managed rule still present: %+v", rules)
		}
	}

	if len(exec.calls()) != 2 {
		t.Fatalf("expected 2 applies, got %v", exec.calls())
	}
}

func TestUpdateProxyPorts_ExcludeMode(t *testing.T) {
	h, _, _, _ := setupProxyPortsTest(t)
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"exclude","ports":"6881:6889","udp_ports":""}`))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	mode, ports := h.readActivePorts()
	if mode != "exclude" || ports != "6881:6889" {
		t.Fatalf("expected mode=exclude, got mode=%q ports=%q", mode, ports)
	}

	// udp_ports rejected in exclude mode.
	req = httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"exclude","ports":"6881","udp_ports":"6881"}`))
	rr = httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("udp_ports in exclude mode must be rejected, got %d", rr.Code)
	}
}

func TestUpdateProxyPorts_ReusesBalancerTag(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	rules := sampleRules()
	rules[2]["balancerTag"] = "my-balancer"
	writeRouting(t, xrayDir, rules)

	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"proxying","ports":"80,443","udp_ports":"443"}`))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	doc, _ := h.readRoutingDoc()
	for _, r := range routingRules(doc) {
		if name, _ := r["name"].(string); name == ManagedUDPRuleName {
			if bt, _ := r["balancerTag"].(string); bt != "my-balancer" {
				t.Fatalf("expected reused balancer tag, got %q", bt)
			}
			return
		}
	}
	t.Fatal("managed rule not found")
}

func TestUpdateProxyPorts_ApplyFailureReported(t *testing.T) {
	h, _, _, _ := setupProxyPortsTest(t)
	// Overwrite executor with a failing one.
	h.executor = &failingExecutor{}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(`{"mode":"proxying","ports":"80,443","udp_ports":""}`))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	// Config still saved.
	if _, ports := h.readActivePorts(); ports != "80,443" {
		t.Fatalf("list must stay saved despite apply failure, got %q", ports)
	}
}

type failingExecutor struct{}

func (f *failingExecutor) Execute(_ context.Context, _ string, _ ...string) (string, error) {
	return "boom", context.DeadlineExceeded
}

// envRecordingExecutor delegates to fakeExecutor but records env passed to
// ExecuteWithEnv.
type envRecordingExecutor struct {
	*fakeExecutor
	env []string
}

func (e *envRecordingExecutor) ExecuteWithEnv(_ context.Context, env []string, name string, args ...string) (string, error) {
	e.env = append(e.env, env...)
	return e.Execute(context.Background(), name, args...)
}

// geoFailExecutor fails `xray -test` with a geosite asset error (simulates a
// bare xray run without XRAY_LOCATION_ASSET).
type geoFailExecutor struct{}

func (g *geoFailExecutor) Execute(_ context.Context, _ string, _ ...string) (string, error) {
	return "common/geodata: failed to open geosite_v2fly.dat > stat /opt/sbin/geosite_v2fly.dat: no such file or directory", errors.New("xray failed")
}

func TestValidateRoutingConfig_UsesAssetEnv(t *testing.T) {
	h, _, _, xrayDir := setupProxyPortsTest(t)
	// xkeen layout: geo files in <confdir>/../dat.
	assetDir := filepath.Join(filepath.Dir(xrayDir), "dat")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "geosite_v2fly.dat"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	er := &envRecordingExecutor{fakeExecutor: &fakeExecutor{}}
	h.executor = er

	if err := h.validateRoutingConfig(); err != nil {
		t.Fatalf("validateRoutingConfig: %v", err)
	}
	found := false
	for _, kv := range er.env {
		if kv == "XRAY_LOCATION_ASSET="+assetDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected XRAY_LOCATION_ASSET=%s in env, got %v", assetDir, er.env)
	}
	calls := er.calls()
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "xray -test -confdir") {
		t.Fatalf("expected a single xray -test call, got %v", calls)
	}
}

func TestValidateRoutingConfig_GeoFailureSkipped(t *testing.T) {
	h, _, _, _ := setupProxyPortsTest(t)
	h.executor = &geoFailExecutor{}
	if err := h.validateRoutingConfig(); err != nil {
		t.Fatalf("geosite failure is environmental and must be skipped, got %v", err)
	}
}

func TestXrayAssetDir(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", "")
	dir := t.TempDir()
	confDir := filepath.Join(dir, "configs")
	assetDir := filepath.Join(dir, "dat")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "geosite_v2fly.dat"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := xrayAssetDir(confDir); got != assetDir {
		t.Fatalf("xrayAssetDir(%q) = %q, want %q", confDir, got, assetDir)
	}
	if got := xrayAssetDir(t.TempDir()); got != "" {
		t.Fatalf("xrayAssetDir with no geo files = %q, want \"\"", got)
	}
}

// failXrayExecutor records calls like fakeExecutor but makes `xray -test`
// fail (as if the merged routing config were rejected).
type failXrayExecutor struct {
	*fakeExecutor
}

func (e *failXrayExecutor) Execute(ctx context.Context, name string, args ...string) (string, error) {
	if name == "xray" {
		return "invalid config", errors.New("xray rejected config")
	}
	return e.fakeExecutor.Execute(ctx, name, args...)
}

func TestUpdateProxyPorts_NoOpSkipsRestart(t *testing.T) {
	h, exec, xkeenDir, xrayDir := setupProxyPortsTest(t)
	// Routing file already carries the managed rule in the correct string
	// syntax at the right position, and the .lst already matches the request.
	rules := sampleRules()
	managed := map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        "50000-51000",
		"balancerTag": "default-balancer",
	}
	rules = append(rules, nil)
	copy(rules[2:], rules[1:])
	rules[1] = managed
	writeRouting(t, xrayDir, rules)
	if err := writePortListFile(filepath.Join(xkeenDir, "port_proxying.lst"), "80,443,50000:51000"); err != nil {
		t.Fatalf("write list: %v", err)
	}

	body := `{"mode":"proxying","ports":"80,443,50000:51000","udp_ports":"50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	if calls := exec.calls(); len(calls) != 0 {
		t.Fatalf("no-op save must not restart xkeen, got %v", calls)
	}
}

func TestUpdateProxyPorts_LegacyRuleRepairedOnSave(t *testing.T) {
	h, exec, xkeenDir, xrayDir := setupProxyPortsTest(t)
	// Simulate the pre-fix state: managed rule written with the broken array
	// form (the bug that stopped the VPN) while the .lst already matches.
	rules := sampleRules()
	rules = append(rules, map[string]interface{}{
		"type":        "field",
		"name":        ManagedUDPRuleName,
		"network":     "udp",
		"port":        []interface{}{"50000:51000"},
		"balancerTag": "default-balancer",
	})
	writeRouting(t, xrayDir, rules)
	if err := writePortListFile(filepath.Join(xkeenDir, "port_proxying.lst"), "80,443,50000:51000"); err != nil {
		t.Fatalf("write list: %v", err)
	}

	body := `{"mode":"proxying","ports":"80,443,50000:51000","udp_ports":"50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// Legacy array rule rewritten to the Xray string syntax.
	doc, ok := h.readRoutingDoc()
	if !ok {
		t.Fatal("routing file missing")
	}
	for _, r := range routingRules(doc) {
		if name, _ := r["name"].(string); name != ManagedUDPRuleName {
			continue
		}
		port, ok := r["port"].(string)
		if !ok || port != "50000-51000" {
			t.Fatalf("legacy rule must be rewritten to string \"50000-51000\", got %#v", r["port"])
		}
		// Lists were unchanged, so the repair itself must restart xkeen
		// (xray pre-flight calls are filtered out).
		var xkeenCalls []string
		for _, c := range exec.calls() {
			if strings.HasPrefix(c, "xkeen") {
				xkeenCalls = append(xkeenCalls, c)
			}
		}
		if len(xkeenCalls) != 1 || xkeenCalls[0] != "xkeen -restart" {
			t.Fatalf("expected exactly one `xkeen -restart`, got %v", exec.calls())
		}
		return
	}
	t.Fatal("managed rule not found")
}

func TestUpdateProxyPorts_RoutingValidationRollback(t *testing.T) {
	h, _, xkeenDir, xrayDir := setupProxyPortsTest(t)
	writeRouting(t, xrayDir, sampleRules())
	if err := writePortListFile(filepath.Join(xkeenDir, "port_proxying.lst"), "80,443"); err != nil {
		t.Fatalf("write list: %v", err)
	}
	routingBefore, _ := os.ReadFile(filepath.Join(xrayDir, "05_routing.json"))

	fe := &failXrayExecutor{fakeExecutor: &fakeExecutor{}}
	h.executor = fe

	body := `{"mode":"proxying","ports":"80,443,8443","udp_ports":"8443"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body = %s)", rr.Code, rr.Body.String())
	}

	// Routing file restored from the backup taken right before the rewrite.
	routingAfter, _ := os.ReadFile(filepath.Join(xrayDir, "05_routing.json"))
	if !bytes.Equal(routingAfter, routingBefore) {
		t.Fatal("routing file must be rolled back after failed validation")
	}

	// Lists restored too — the new port must not remain on disk.
	raw, _ := os.ReadFile(filepath.Join(xkeenDir, "port_proxying.lst"))
	if string(raw) != "80\n443\n" {
		t.Fatalf("proxying list must be rolled back, got %q", string(raw))
	}

	// No xkeen command may run after the rollback (no restart).
	for _, c := range fe.calls() {
		if strings.HasPrefix(c, "xkeen") {
			t.Fatalf("no xkeen command must run after rollback, got %v", fe.calls())
		}
	}
}
