package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

func TestDiscoverDNSUpstream(t *testing.T) {
	orig := dnsSyncNDMCFn
	defer func() { dnsSyncNDMCFn = orig }()
	dnsSyncNDMCFn = func(_ context.Context, _ ...string) ([]byte, error) {
		// zone-scoped entry BEFORE default — catches parsers that latch the
		// first non-loopback IP regardless of its domain.
		return []byte("address: 8.8.8.8\ndomain: local\naddress: 127.0.0.1\ndomain: \naddress: 192.168.5.10\ndomain: \n"), nil
	}
	if got := discoverDNSUpstream(); got != "192.168.5.10" {
		t.Fatalf("expected first non-loopback DEFAULT DNS, got %q", got)
	}
}

func TestSyncDNSForward_Zones(t *testing.T) {
	origRun, origLook := dnsSyncNDMCFn, dnsSyncLookPathFn
	defer func() { dnsSyncNDMCFn, dnsSyncLookPathFn = origRun, origLook }()
	dnsSyncLookPathFn = func(string) (string, error) { return "/bin/ndmc", nil }
	var cmds []string
	dnsSyncNDMCFn = func(_ context.Context, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "show ip name-server") {
			return []byte("address: 10.0.0.1\ndomain: \naddress: 10.0.0.9\ndomain: userzone\naddress: 10.0.0.1\ndomain: local\n"), nil
		}
		return nil, nil
	}
	cfg := &config.Config{AWDNSForward: true, AWDNSZones: []string{"local", ".home", "lan"}, AWDNSManagedZones: []string{"local", "oldzone"}}
	status, err := SyncDNSForward(cfg, "") // no config save in test
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !strings.Contains(status, "10.0.0.1") {
		t.Fatalf("status missing upstream: %s", status)
	}
	joined := strings.Join(cmds, "\n")
	for _, want := range []string{
		"ip name-server 10.0.0.1 local",
		"ip name-server 10.0.0.1 home",
		"ip name-server 10.0.0.1 lan",
		"no ip name-server 10.0.0.1 oldzone", // stale OWNED rule removed
		"system configuration save",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in commands:\n%s", want, joined)
		}
	}
	// User-configured zone must NOT be touched.
	if strings.Contains(joined, "no ip name-server 10.0.0.9 userzone") {
		t.Fatalf("must not remove user-configured zones:\n%s", joined)
	}
	// Ownership updated to current zones.
	if len(cfg.AWDNSManagedZones) != 3 {
		t.Fatalf("managed zones not updated: %v", cfg.AWDNSManagedZones)
	}
}

func TestSyncDNSForward_DisabledRemovesOnlyOwned(t *testing.T) {
	origRun, origLook := dnsSyncNDMCFn, dnsSyncLookPathFn
	defer func() { dnsSyncNDMCFn, dnsSyncLookPathFn = origRun, origLook }()
	dnsSyncLookPathFn = func(string) (string, error) { return "/bin/ndmc", nil }
	var cmds []string
	dnsSyncNDMCFn = func(_ context.Context, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "show ip name-server") {
			return []byte("address: 10.0.0.1\ndomain: \naddress: 10.0.0.1\ndomain: local\naddress: 10.0.0.9\ndomain: userzone\n"), nil
		}
		return nil, nil
	}
	cfg := &config.Config{AWDNSForward: false, AWDNSManagedZones: []string{"local"}}
	if _, err := SyncDNSForward(cfg, ""); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "no ip name-server 10.0.0.1 local") {
		t.Fatalf("owned stale zone must be removed:\n%s", joined)
	}
	if strings.Contains(joined, "userzone") {
		t.Fatalf("user zone must be untouched:\n%s", joined)
	}
	for _, line := range strings.Split(joined, "\n") {
		if strings.HasPrefix(line, "-c ip name-server") && !strings.Contains(line, "10.0.0.1") {
			t.Fatalf("must not add zones when disabled:\n%s", joined)
		}
	}
}

func TestSyncDNSForward_UpstreamChanged(t *testing.T) {
	origRun, origLook := dnsSyncNDMCFn, dnsSyncLookPathFn
	defer func() { dnsSyncNDMCFn, dnsSyncLookPathFn = origRun, origLook }()
	dnsSyncLookPathFn = func(string) (string, error) { return "/bin/ndmc", nil }
	var cmds []string
	dnsSyncNDMCFn = func(_ context.Context, args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "show ip name-server") {
			return []byte("address: 10.0.0.2\ndomain: \n"), nil // new upstream
		}
		return nil, nil
	}
	cfg := &config.Config{AWDNSForward: true, AWDNSZones: []string{"local"},
		AWDNSManagedZones:   []string{"local", "oldzone"},
		AWDNSManagedZoneIPs: map[string]string{"local": "10.0.0.1", "oldzone": "10.0.0.1"}}
	if _, err := SyncDNSForward(cfg, ""); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmds, "\n")
	// stale rule must be removed using the OLD recorded IP, not the new one
	if !strings.Contains(joined, "no ip name-server 10.0.0.1 oldzone") {
		t.Fatalf("stale rule removal must use recorded IP:\n%s", joined)
	}
	// new zone rule added with the new IP
	if !strings.Contains(joined, "no ip name-server 10.0.0.1 local") {
		t.Fatalf("old zone rule must be removed before adding with new IP:\n%s", joined)
	}
	if !strings.Contains(joined, "ip name-server 10.0.0.2 local") {
		t.Fatalf("zone must be added with current upstream:\n%s", joined)
	}
	if cfg.AWDNSManagedZoneIPs["local"] != "10.0.0.2" {
		t.Fatalf("recorded IP not updated: %v", cfg.AWDNSManagedZoneIPs)
	}
}
