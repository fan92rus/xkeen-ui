package handlers

import (
	"context"
	"encoding/json"
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

func TestPortListSubset(t *testing.T) {
	if !portListSubset("50000:51000", "80,443,50000:51000") {
		t.Fatal("expected subset to hold")
	}
	if portListSubset("50000:51000", "80,443") {
		t.Fatal("expected subset to fail")
	}
	if !portListSubset("", "80,443") {
		t.Fatal("empty subset must hold")
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
	h, exec, _, xrayDir := setupProxyPortsTest(t)
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

	// .lst file written correctly.
	mode, ports := h.readActivePorts()
	if mode != "proxying" || ports != "80,443,50000:51000" {
		t.Fatalf("list file not written correctly: mode=%q ports=%q", mode, ports)
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
	if rules[2]["outboundTag"] != "direct" {
		t.Fatalf("udp->direct catch-all must follow the managed rule: %+v", rules[2])
	}

	// Apply triggered via xkeen -restart.
	calls := exec.calls()
	if len(calls) != 1 || calls[0] != "xkeen -restart" {
		t.Fatalf("expected exactly one `xkeen -restart`, got %v", calls)
	}
}

func TestUpdateProxyPorts_UDPNotSubset(t *testing.T) {
	h, _, _, _ := setupProxyPortsTest(t)
	body := `{"mode":"proxying","ports":"80,443","udp_ports":"50000:51000"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/proxy-ports", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.UpdateProxyPorts(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
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
