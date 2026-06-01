package subscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------- Scheduler creation ----------

func TestNewScheduler(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	s := NewScheduler(store, fetcher)
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
}

// ---------- Stop/Start ----------

func TestScheduler_StopWithoutStart(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	s := NewScheduler(store, fetcher)
	// Should not panic
	s.Stop()
}

func TestScheduler_StartStopIdempotent(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	s := NewScheduler(store, fetcher)
	s.Start()
	time.Sleep(50 * time.Millisecond)

	// Double stop should be fine
	s.Stop()
	s.Stop()
}

// ---------- RefreshOne ----------

func TestRefreshOne_Success(t *testing.T) {
	// Create a test server that returns a vless subscription
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Hiddify" {
			t.Error("expected Hiddify User-Agent")
		}

		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#%F0%9F%87%A9%F0%9F%87%AA%20Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Test", URL: server.URL, Enabled: true}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne(sub.ID)
	if err != nil {
		t.Fatalf("RefreshOne: %v", err)
	}

	updated, _ := store.GetSubscription(sub.ID)
	if updated.LastFetch.IsZero() {
		t.Error("expected LastFetch to be set")
	}
	if updated.LastError != "" {
		t.Errorf("unexpected LastError: %q", updated.LastError)
	}
	if updated.ProxyCount == 0 {
		t.Error("expected ProxyCount > 0")
	}
}

func TestRefreshOne_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent subscription")
	}
}

func TestRefreshOne_BadURL(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Bad", URL: "http://127.0.0.1:1/impossible", Enabled: true}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne(sub.ID)
	if err == nil {
		t.Error("expected error for unreachable URL")
	}

	updated, _ := store.GetSubscription(sub.ID)
	if updated.LastError == "" {
		t.Error("expected LastError to be set on failure")
	}
	if updated.LastFetch.IsZero() {
		t.Error("expected LastFetch to be set even on failure")
	}
}

// ---------- RefreshAll ----------

func TestRefreshAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.AddSubscription(&Subscription{Name: "Sub1", URL: server.URL, Enabled: true})
	store.AddSubscription(&Subscription{Name: "Sub2", URL: server.URL, Enabled: true})
	// Disabled subscription should be skipped
	store.AddSubscription(&Subscription{Name: "Disabled", URL: "http://bad", Enabled: false})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshAll()
	if err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}

	cfg := store.GetConfig()
	for _, sub := range cfg.Subscriptions {
		if sub.Name == "Disabled" {
			if !sub.LastFetch.IsZero() {
				t.Error("disabled subscription should not be fetched")
			}
			continue
		}
		if sub.LastFetch.IsZero() {
			t.Errorf("subscription %q should have LastFetch set", sub.Name)
		}
	}
}

// ---------- OnUpdate callback ----------

func TestOnUpdate_CalledOnRefreshOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Test", URL: server.URL, Enabled: true}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	callbackCalled := false
	sched.OnUpdate = func() {
		callbackCalled = true
	}

	// RefreshOne itself doesn't call OnUpdate — only RefreshAll does.
	// But let's verify RefreshAll calls it.
	store.AddSubscription(&Subscription{Name: "Sub2", URL: server.URL, Enabled: true})
	sched.RefreshAll()

	if !callbackCalled {
		t.Error("expected OnUpdate callback to be called")
	}
}

// ---------- Integration: Fetcher with custom client + scheduler ----------

func TestScheduler_FetchWithCustomClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://uuid@1.2.3.4:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	sub := &Subscription{Name: "Test", URL: server.URL, Enabled: true}
	store.AddSubscription(sub)

	client := &http.Client{Timeout: 5 * time.Second}
	fetcher := NewFetcherWithClient(client)
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshOne(sub.ID)
	if err != nil {
		t.Fatalf("RefreshOne with custom client: %v", err)
	}
}

// ---------- Auto-Apply ----------

func TestAutoApply_GetSet(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	enabled, cronExpr := store.GetAutoApply()
	if enabled {
		t.Error("expected disabled by default")
	}
	if cronExpr != "" {
		t.Error("expected empty cron by default")
	}

	if err := store.SetAutoApply(true, "0 */6 * * *"); err != nil {
		t.Fatalf("SetAutoApply: %v", err)
	}

	enabled, cronExpr = store.GetAutoApply()
	if !enabled {
		t.Error("expected enabled")
	}
	if cronExpr != "0 */6 * * *" {
		t.Errorf("expected cron '0 */6 * * *', got %q", cronExpr)
	}

	// Disable
	store.SetAutoApply(false, "")
	enabled, _ = store.GetAutoApply()
	if enabled {
		t.Error("expected disabled")
	}
}

func TestScheduler_UpdateAutoApply_InvalidCron(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.UpdateAutoApply(true, "invalid cron expression!!")
	if err == nil {
		t.Error("expected error for invalid cron")
	}
}

func TestScheduler_UpdateAutoApply_Disable(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	// Enable
	if err := sched.UpdateAutoApply(true, "0 */6 * * *"); err != nil {
		t.Fatalf("UpdateAutoApply enable: %v", err)
	}

	// Disable
	if err := sched.UpdateAutoApply(false, ""); err != nil {
		t.Fatalf("UpdateAutoApply disable: %v", err)
	}

	nextRun := sched.GetNextRun()
	if !nextRun.IsZero() {
		t.Error("expected zero next_run when disabled")
	}
}

func TestScheduler_UpdateAutoApply_ValidCron(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	if err := sched.UpdateAutoApply(true, "*/5 * * * *"); err != nil {
		t.Fatalf("UpdateAutoApply: %v", err)
	}

	nextRun := sched.GetNextRun()
	if nextRun.IsZero() {
		t.Error("expected non-zero next_run")
	}

	// Clean up
	sched.Stop()
}

func TestScheduler_SetXrayDir(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	sched.SetXrayDir("/opt/etc/xray")
	if sched.xrayDir != "/opt/etc/xray" {
		t.Errorf("expected /opt/etc/xray, got %q", sched.xrayDir)
	}
}

// ---------- runAutoApply ----------

func TestScheduler_RunAutoApply_Success(t *testing.T) {
	// Create a test server that returns vless proxies
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#%F0%9F%87%A9%F0%9F%87%AA%20Standard"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	// Run refresh first to populate proxies
	if err := sched.RefreshAll(); err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}
	proxies := store.GetProxies()
	if len(proxies) == 0 {
		t.Fatal("expected proxies after RefreshAll")
	}

	// Run auto-apply
	sched.runAutoApply()

	// Verify files written to disk
	outboundsData, err := os.ReadFile(filepath.Join(dir, "04_outbounds.json"))
	if err != nil {
		t.Fatalf("expected 04_outbounds.json: %v", err)
	}
	var outbounds map[string]json.RawMessage
	if err := json.Unmarshal(outboundsData, &outbounds); err != nil {
		t.Fatalf("parse outbounds: %v", err)
	}
	if _, ok := outbounds["outbounds"]; !ok {
		t.Error("expected 'outbounds' key in outbounds file")
	}

	routingData, err := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	if err != nil {
		t.Fatalf("expected 05_routing.json: %v", err)
	}
	var routing map[string]json.RawMessage
	if err := json.Unmarshal(routingData, &routing); err != nil {
		t.Fatalf("parse routing: %v", err)
	}
	if _, ok := routing["routing"]; !ok {
		t.Error("expected 'routing' key in routing file")
	}

	// Default strategy is "all" — no observatory should be written
	if _, err := os.Stat(filepath.Join(dir, "07_observatory.json")); !os.IsNotExist(err) {
		t.Error("expected no observatory file for 'all' strategy")
	}
}

func TestScheduler_RunAutoApply_WithObservatory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#%F0%9F%87%A9%F0%9F%87%AA%20Standard"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})
	store.SetStrategy(&RoutingStrategy{Type: "leastping", FallbackTag: "direct"})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	sched.RefreshAll()
	sched.runAutoApply()

	// Observatory should be written for leastping
	obsData, err := os.ReadFile(filepath.Join(dir, "07_observatory.json"))
	if err != nil {
		t.Fatalf("expected 07_observatory.json for leastping: %v", err)
	}
	var obs map[string]interface{}
	if err := json.Unmarshal(obsData, &obs); err != nil {
		t.Fatalf("parse observatory: %v", err)
	}
	if _, ok := obs["observatory"]; !ok {
		t.Error("expected 'observatory' key")
	}
}

func TestScheduler_RunAutoApply_NoProxies(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	// No subscriptions, no proxies — should skip gracefully
	sched.runAutoApply()

	// No files should be written
	if _, err := os.Stat(filepath.Join(dir, "04_outbounds.json")); !os.IsNotExist(err) {
		t.Error("expected no outbounds file when no proxies")
	}
}

func TestScheduler_RunAutoApply_WriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	// No xrayDir set — should fail gracefully

	sched.RefreshAll()
	sched.runAutoApply()

	// Should not panic; files not written
	if _, err := os.Stat("04_outbounds.json"); !os.IsNotExist(err) {
		t.Error("expected no outbounds file without xrayDir")
	}
}

func TestScheduler_RunAutoApply_RemovesObservatory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})
	// Start with leastping (creates observatory)
	store.SetStrategy(&RoutingStrategy{Type: "leastping", FallbackTag: "direct"})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	sched.RefreshAll()
	sched.runAutoApply()

	// Observatory exists
	if _, err := os.Stat(filepath.Join(dir, "07_observatory.json")); os.IsNotExist(err) {
		t.Fatal("expected observatory file for leastping")
	}

	// Switch to "all" — observatory should be removed
	store.SetStrategy(&RoutingStrategy{Type: "all", FallbackTag: "direct"})
	sched.runAutoApply()

	if _, err := os.Stat(filepath.Join(dir, "07_observatory.json")); !os.IsNotExist(err) {
		t.Error("expected observatory file to be removed when strategy doesn't need it")
	}
}

func TestScheduler_RunAutoApply_SetsGeneratedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	before := time.Now()
	sched.RefreshAll()
	sched.runAutoApply()

	cfg := store.GetConfig()
	if cfg.GeneratedAt.Before(before) {
		t.Error("expected GeneratedAt to be updated after runAutoApply")
	}
}

func TestScheduler_RunAutoApply_OnUpdateCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	called := false
	sched.OnUpdate = func() { called = true }

	sched.RefreshAll()
	sched.runAutoApply()

	if !called {
		t.Error("expected OnUpdate callback to be called after runAutoApply")
	}
}

func TestScheduler_RunAutoApply_AllFilteredOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality&pbk=fakePublicKeyBase64EncodedHere_a1b2c3&fp=chrome&sni=example.com&sid=aabb112233445566&flow=xtls-rprx-vision#%E2%9A%A1%20Fast"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "Test", URL: server.URL, Enabled: true})
	// Exclude by regex which all proxies match (⚡ in remarks)
	store.SetFilters(&Filter{ExcludeRegexes: []string{"⚡"}})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	sched.RefreshAll()
	sched.runAutoApply()

	// With profiles model, outbounds are always written (filtering is via balancer selectors)
	if _, err := os.Stat(filepath.Join(dir, "04_outbounds.json")); os.IsNotExist(err) {
		t.Error("expected outbounds file to be written (profiles handle filtering)")
	}
}

func TestScheduler_WriteConfigFiles_PreservesExistingRouting(t *testing.T) {
	dir := t.TempDir()

	// Write existing routing with a custom rule
	existingRouting := map[string]interface{}{
		"routing": map[string]interface{}{
			"domainStrategy": "AsIs",
			"rules": []interface{}{
				map[string]interface{}{
					"type":        "field",
					"outboundTag": "my-custom",
					"domain":      []string{"example.com"},
				},
			},
		},
	}
	routingJSON, _ := json.MarshalIndent(existingRouting, "", "  ")
	os.WriteFile(filepath.Join(dir, "05_routing.json"), routingJSON, 0644)

	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	proxies := []*ProxyEntry{
		{Tag: "proxy-de-1", Protocol: "vless", Country: "DE", Outbound: json.RawMessage(`{"protocol":"vloss","settings":{}}`)},
	}
	store.SetProxies(proxies)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	err := sched.writeConfigFiles(proxies, []Profile{{ID: "default", IsDefault: true, Enabled: true, Strategy: RoutingStrategy{Type: "all"}}})
	if err != nil {
		t.Fatalf("writeConfigFiles: %v", err)
	}

	// Read routing and verify custom rule preserved
	data, err := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	if err != nil {
		t.Fatalf("read routing: %v", err)
	}
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	routing := result["routing"].(map[string]interface{})
	rules := routing["rules"].([]interface{})

	// Should have: custom rule preserved as-is (no new rules added)
	foundCustom := false
	for _, r := range rules {
		rule := r.(map[string]interface{})
		if tag, ok := rule["outboundTag"]; ok && tag == "my-custom" {
			foundCustom = true
		}
	}
	if !foundCustom {
		t.Error("expected custom routing rule to be preserved")
	}
	// No new proxy rule added when existing rules are preserved
	if len(rules) != 1 {
		t.Errorf("expected 1 rule (custom only), got %d", len(rules))
	}
}

func TestScheduler_WriteConfigFiles_BalancerMode(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	proxies := []*ProxyEntry{
		{Tag: "proxy-de-1", Protocol: "vless", Country: "DE", Outbound: json.RawMessage(`{"protocol":"vloss","settings":{}}`)},
		{Tag: "proxy-nl-1", Protocol: "vless", Country: "NL", Outbound: json.RawMessage(`{"protocol":"vloss","settings":{}}`)},
	}

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.SetXrayDir(dir)

	err := sched.writeConfigFiles(proxies, []Profile{{ID: "default", IsDefault: true, Enabled: true, Strategy: RoutingStrategy{Type: "random", FallbackTag: "direct"}}})
	if err != nil {
		t.Fatalf("writeConfigFiles: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	routing := result["routing"].(map[string]interface{})

	balancers, ok := routing["balancers"].([]interface{})
	if !ok || len(balancers) == 0 {
		t.Fatal("expected balancers in routing for random strategy")
	}
	balancer := balancers[0].(map[string]interface{})
	if balancer["tag"] != "default-balancer" {
		t.Error("expected balancer tag 'default-balancer'")
	}

	// Rules: ad-block + fallback balancer rule
	rules := routing["rules"].([]interface{})
	if len(rules) != 2 {
		t.Errorf("expected 2 rules (ad-block + fallback), got %d", len(rules))
	}
}

func TestScheduler_WriteConfigFiles_NoXrayDir(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	// xrayDir is empty

	err := sched.writeConfigFiles([]*ProxyEntry{{Tag: "test"}}, []Profile{{ID: "default", IsDefault: true, Enabled: true, Strategy: RoutingStrategy{Type: "all"}}})
	if err == nil {
		t.Error("expected error when xrayDir is empty")
	}
}

// ---------- Concurrent refresh ----------

func TestScheduler_ConcurrentRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Add 5 subscriptions
	for i := 0; i < 5; i++ {
		store.AddSubscription(&Subscription{Name: fmt.Sprintf("Sub%d", i), URL: server.URL, Enabled: true})
	}

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	// Refresh all concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, 5)
	cfg := store.GetConfig()
	for _, sub := range cfg.Subscriptions {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := sched.RefreshOne(id); err != nil {
				errCh <- err
			}
		}(sub.ID)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent RefreshOne error: %v", err)
	}

	// All subscriptions should have LastFetch set
	cfg = store.GetConfig()
	for _, sub := range cfg.Subscriptions {
		if sub.LastFetch.IsZero() {
			t.Errorf("subscription %q should have LastFetch set", sub.Name)
		}
	}
}

// ---------- Interval checker ----------

func TestScheduler_IntervalChecker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp&security=reality#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Add subscription with very short interval and zero LastFetch
	sub := &Subscription{Name: "AutoRefresh", URL: server.URL, Enabled: true, Interval: 1}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.Start()
	defer sched.Stop()

	// Wait for the interval checker to trigger (runs immediately on Start)
	time.Sleep(500 * time.Millisecond)

	updated, err := store.GetSubscription(sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if updated.LastFetch.IsZero() {
		t.Error("expected LastFetch to be set by interval checker")
	}
	if updated.ProxyCount == 0 {
		t.Error("expected ProxyCount > 0 after auto-refresh")
	}
}

func TestScheduler_IntervalChecker_SkipsDisabledSubscriptions(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Disabled subscription with interval
	sub := &Subscription{Name: "Disabled", URL: "http://bad", Enabled: false, Interval: 1}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.Start()
	defer sched.Stop()

	time.Sleep(200 * time.Millisecond)

	updated, _ := store.GetSubscription(sub.ID)
	if !updated.LastFetch.IsZero() {
		t.Error("disabled subscription should not be auto-refreshed")
	}
}

func TestScheduler_IntervalChecker_SkipsZeroInterval(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	sub := &Subscription{Name: "ManualOnly", URL: "http://bad", Enabled: true, Interval: 0}
	store.AddSubscription(sub)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	sched.Start()
	defer sched.Stop()

	time.Sleep(200 * time.Millisecond)

	updated, _ := store.GetSubscription(sub.ID)
	if !updated.LastFetch.IsZero() {
		t.Error("zero-interval subscription should not be auto-refreshed")
	}
}

func TestScheduler_Stop_StopsCron(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	// Enable cron
	if err := sched.UpdateAutoApply(true, "*/5 * * * *"); err != nil {
		t.Fatalf("UpdateAutoApply: %v", err)
	}

	nextRun := sched.GetNextRun()
	if nextRun.IsZero() {
		t.Error("expected non-zero next_run after enable")
	}

	sched.Stop()

	nextRun = sched.GetNextRun()
	if !nextRun.IsZero() {
		t.Error("expected zero next_run after stop")
	}
}

func TestScheduler_GetNextRun_NoCron(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	nextRun := sched.GetNextRun()
	if !nextRun.IsZero() {
		t.Error("expected zero time when no cron active")
	}
}

func TestScheduler_UpdateAutoApply_CronSwap(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	// Enable with one cron
	if err := sched.UpdateAutoApply(true, "0 * * * *"); err != nil {
		t.Fatalf("first enable: %v", err)
	}

	// Swap to different cron
	if err := sched.UpdateAutoApply(true, "0 */2 * * *"); err != nil {
		t.Fatalf("swap: %v", err)
	}

	nextRun := sched.GetNextRun()
	if nextRun.IsZero() {
		t.Error("expected non-zero next_run after swap")
	}

	sched.Stop()
}

func TestScheduler_RefreshAll_FailureContinues(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 1 {
			// First request fails
			w.WriteHeader(500)
			return
		}
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	store.AddSubscription(&Subscription{Name: "FailsFirst", URL: server.URL, Enabled: true})
	store.AddSubscription(&Subscription{Name: "WorksSecond", URL: server.URL, Enabled: true})

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	err := sched.RefreshAll()
	if err != nil {
		t.Fatalf("RefreshAll should not return error even if some fail: %v", err)
	}

	// At least one should succeed
	proxies := store.GetProxies()
	if len(proxies) == 0 {
		t.Error("expected some proxies from successful fetch")
	}
}

func TestScheduler_RefreshAll_SkipsDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lines := "vless://a1b2c3d4-e5f6-4a56-ef12-ef1234567890@10.0.0.1:443?type=tcp#Test"
		encoded := base64.StdEncoding.EncodeToString([]byte(lines))
		w.Write([]byte(encoded))
	}))
	defer server.Close()

	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	enabled := &Subscription{Name: "Enabled", URL: server.URL, Enabled: true}
	disabled := &Subscription{Name: "Disabled", URL: server.URL, Enabled: false}
	store.AddSubscription(enabled)
	store.AddSubscription(disabled)

	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	sched.RefreshAll()

	enabledSub, _ := store.GetSubscription(enabled.ID)
	disabledSub, _ := store.GetSubscription(disabled.ID)

	if enabledSub.LastFetch.IsZero() {
		t.Error("enabled sub should be fetched")
	}
	if !disabledSub.LastFetch.IsZero() {
		t.Error("disabled sub should not be fetched")
	}
}

// ---------- Context timeout ----------

func TestScheduler_FetcherContextCancellation(t *testing.T) {
	// Server that sleeps forever
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer server.Close()

	fetcher := NewFetcher()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestScheduler_Stop_StopsIntervalChecker(t *testing.T) {
	// Verify that Stop() terminates the interval checker goroutine
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)

	// Start the scheduler — this starts the interval checker goroutine
	sched.Start()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Stop should cancel the context and wait for the goroutine
	done := make(chan struct{})
	go func() {
		sched.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Stop returned — interval checker goroutine terminated
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung — interval checker goroutine not stopped")
	}
}

func TestScheduler_StartStopMultipleTimes(t *testing.T) {
	// Verify Start/Stop can be called multiple times without leaking goroutines
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	for i := 0; i < 5; i++ {
		sched := NewScheduler(store, fetcher)
		sched.Start()
		time.Sleep(50 * time.Millisecond)
		sched.Stop()
	}
}

// --- enableCron ---

func TestScheduler_EnableCron(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	defer sched.Stop()

	// Enable cron with a valid expression
	err := sched.enableCron("0 */6 * * *")
	if err != nil {
		t.Fatalf("enableCron failed: %v", err)
	}

	sched.mu.Lock()
	hasCron := sched.cron != nil
	sched.mu.Unlock()
	if !hasCron {
		t.Error("expected cron to be initialized after enableCron")
	}

	// Next run should be available
	nextRun := sched.GetNextRun()
	if nextRun.IsZero() {
		t.Error("GetNextRun should return non-zero time after enableCron")
	}
}

func TestScheduler_EnableCron_InvalidExpression(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()
	sched := NewScheduler(store, fetcher)
	defer sched.Stop()

	err := sched.enableCron("invalid-cron-expression")
	if err == nil {
		t.Error("expected error for invalid cron expression")
	}

	sched.mu.Lock()
	hasCron := sched.cron != nil
	sched.mu.Unlock()
	if hasCron {
		t.Error("cron should be nil after failed enableCron")
	}
}

// --- Start with existing cron config ---

func TestScheduler_Start_WithExistingCron(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "subscriptions.json")
	store, _ := NewStore(cfgPath)
	fetcher := NewFetcher()

	// Enable auto-apply in config so Start() restores cron
	store.SetAutoApply(true, "0 */12 * * *")

	sched := NewScheduler(store, fetcher)
	sched.Start()
	defer sched.Stop()

	// Give Start time to restore cron
	time.Sleep(200 * time.Millisecond)

	sched.mu.Lock()
	hasCron := sched.cron != nil
	sched.mu.Unlock()
	if !hasCron {
		t.Error("expected cron to be restored after Start() with enabled auto-apply")
	}

	nextRun := sched.GetNextRun()
	if nextRun.IsZero() {
		t.Errorf("GetNextRun should return non-zero time: %v", nextRun)
	}
}

func TestScheduler_Start_WithoutCron(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))
	fetcher := NewFetcher()

	// Auto-apply is disabled by default
	sched := NewScheduler(store, fetcher)
	sched.Start()
	defer sched.Stop()

	time.Sleep(100 * time.Millisecond)

	sched.mu.Lock()
	hasCron := sched.cron != nil
	sched.mu.Unlock()
	if hasCron {
		t.Error("cron should be nil when auto-apply is disabled")
	}
}


