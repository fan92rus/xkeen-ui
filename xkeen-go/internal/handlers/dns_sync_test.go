package handlers

import (
	"strings"
	"testing"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

func TestDiscoverDNSUpstream(t *testing.T) {
	orig := dnsSyncNDMCFn
	defer func() { dnsSyncNDMCFn = orig }()
	dnsSyncNDMCFn = func(args ...string) ([]byte, error) {
		return []byte("           server: \n              address: 127.0.0.1\n               domain: \n              address: 192.168.5.10\n               domain: \n              address: 1.1.1.1\n               domain: local\n"), nil
	}
	if got := discoverDNSUpstream(); got != "192.168.5.10" {
		t.Fatalf("expected first non-loopback default DNS, got %q", got)
	}
}

func TestSyncDNSForward_Zones(t *testing.T) {
	origRun, origLook := dnsSyncNDMCFn, dnsSyncLookPathFn
	defer func() { dnsSyncNDMCFn, dnsSyncLookPathFn = origRun, origLook }()
	dnsSyncLookPathFn = func(string) (string, error) { return "/bin/ndmc", nil }
	var cmds []string
	dnsSyncNDMCFn = func(args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "show ip name-server") {
			return []byte("address: 10.0.0.1\ndomain: \naddress: 10.0.0.1\ndomain: oldzone\n"), nil
		}
		return nil, nil
	}
	cfg := &config.Config{AWDNSForward: true, AWDNSZones: []string{"local", ".home", "lan"}}
	status, err := SyncDNSForward(cfg)
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
		"no ip name-server 10.0.0.1 oldzone",
		"system configuration save",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in commands:\n%s", want, joined)
		}
	}
}

func TestSyncDNSForward_DisabledRemovesStale(t *testing.T) {
	origRun, origLook := dnsSyncNDMCFn, dnsSyncLookPathFn
	defer func() { dnsSyncNDMCFn, dnsSyncLookPathFn = origRun, origLook }()
	dnsSyncLookPathFn = func(string) (string, error) { return "/bin/ndmc", nil }
	var cmds []string
	dnsSyncNDMCFn = func(args ...string) ([]byte, error) {
		cmd := strings.Join(args, " ")
		cmds = append(cmds, cmd)
		if strings.Contains(cmd, "show ip name-server") {
			return []byte("address: 10.0.0.1\ndomain: \naddress: 10.0.0.1\ndomain: local\n"), nil
		}
		return nil, nil
	}
	cfg := &config.Config{AWDNSForward: false}
	if _, err := SyncDNSForward(cfg); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "no ip name-server 10.0.0.1 local") {
		t.Fatalf("stale zone must be removed:\n%s", joined)
	}
	for _, line := range strings.Split(joined, "\n") {
		if strings.HasPrefix(line, "-c ip name-server") {
			t.Fatalf("must not add zones when disabled:\n%s", joined)
		}
	}
}
