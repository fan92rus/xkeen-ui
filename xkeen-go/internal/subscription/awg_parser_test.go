package subscription

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConf(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestParseAWGConf_ServerConfig(t *testing.T) {
	dir := t.TempDir()
	conf := `[Interface]
PrivateKey = abc123
Address = 10.8.0.1/32
ListenPort = 443
MTU = 1420
Jc = 4

[Peer]
PublicKey = xyz789
AllowedIPs = 10.8.0.2/32`
	path := filepath.Join(dir, "server.conf")
	writeConf(t, dir, "server.conf", conf)

	parsed, err := ParseAWGConf(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Interface == nil {
		t.Fatal("expected Interface section")
	}
	if parsed.Interface.Values["ListenPort"] != "443" {
		t.Errorf("ListenPort = %q, want 443", parsed.Interface.Values["ListenPort"])
	}
	if len(parsed.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(parsed.Peers))
	}
	if parsed.Peers[0].Values["PublicKey"] != "xyz789" {
		t.Errorf("PublicKey = %q", parsed.Peers[0].Values["PublicKey"])
	}
}

func TestParseAWGConf_ClientConfig(t *testing.T) {
	dir := t.TempDir()
	conf := `[Interface]
PrivateKey = abc123
Address = 10.8.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = xyz789
Endpoint = 146.120.53.90:443
AllowedIPs = 0.0.0.0/0`
	path := filepath.Join(dir, "warp.conf")
	writeConf(t, dir, "warp.conf", conf)

	parsed, err := ParseAWGConf(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GetListenPort() != 0 {
		t.Errorf("ListenPort = %d, want 0", parsed.GetListenPort())
	}
	if len(parsed.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(parsed.Peers))
	}
	if parsed.Peers[0].Values["Endpoint"] == "" {
		t.Error("expected Endpoint in peer")
	}
}

func TestDetectAWGRole_Server(t *testing.T) {
	conf := &AWGConf{
		Interface: &AWGConfigSection{
			Type:   "Interface",
			Values: map[string]string{"ListenPort": "443"},
		},
		Peers: []*AWGConfigSection{
			{Type: "Peer", Values: map[string]string{"PublicKey": "abc", "AllowedIPs": "10.8.0.2/32"}},
		},
	}
	if role := DetectAWGRole(conf); role != AWGRoleServer {
		t.Errorf("role = %s, want server", role)
	}
}

func TestDetectAWGRole_Client(t *testing.T) {
	conf := &AWGConf{
		Interface: &AWGConfigSection{
			Type:   "Interface",
			Values: map[string]string{"Address": "10.8.0.2/32"},
		},
		Peers: []*AWGConfigSection{
			{Type: "Peer", Values: map[string]string{"Endpoint": "1.2.3.4:443"}},
		},
	}
	if role := DetectAWGRole(conf); role != AWGRoleClient {
		t.Errorf("role = %s, want client", role)
	}
}

func TestDetectAWGRole_NoPeers(t *testing.T) {
	conf := &AWGConf{
		Interface: &AWGConfigSection{
			Type:   "Interface",
			Values: map[string]string{"ListenPort": "443"},
		},
		Peers: nil,
	}
	// No peers, has listen port — technically a server with no clients
	if role := DetectAWGRole(conf); role != AWGRoleServer {
		t.Errorf("role = %s, want server (listen + no endpoint)", role)
	}
}

func TestGetListenPort(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"443", 443},
		{"51820", 51820},
		{"", 0},
		{"abc", 0},
	}
	for _, tt := range tests {
		conf := &AWGConf{
			Interface: &AWGConfigSection{Values: map[string]string{"ListenPort": tt.input}},
		}
		if got := conf.GetListenPort(); got != tt.want {
			t.Errorf("GetListenPort(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestGetTunnelSubnet(t *testing.T) {
	// From peer AllowedIPs
	conf := &AWGConf{
		Peers: []*AWGConfigSection{
			{Values: map[string]string{"AllowedIPs": "10.8.0.5/32"}},
		},
	}
	if subnet := conf.GetTunnelSubnet(); subnet != "10.8.0.0/24" {
		t.Errorf("subnet = %s, want 10.8.0.0/24", subnet)
	}

	// Fallback to Interface Address
	conf2 := &AWGConf{
		Interface: &AWGConfigSection{Values: map[string]string{"Address": "192.168.99.1/24"}},
	}
	if subnet := conf2.GetTunnelSubnet(); subnet != "192.168.99.0/24" {
		t.Errorf("subnet = %s, want 192.168.99.0/24", subnet)
	}

	// Empty config
	conf3 := &AWGConf{}
	if subnet := conf3.GetTunnelSubnet(); subnet != "" {
		t.Errorf("subnet = %s, want empty", subnet)
	}
}

func TestParseAWGConf_MultiplePeers(t *testing.T) {
	dir := t.TempDir()
	conf := `[Interface]
PrivateKey = abc
ListenPort = 443

[Peer]
PublicKey = key1
AllowedIPs = 10.8.0.2/32

[Peer]
PublicKey = key2
AllowedIPs = 10.8.0.3/32

[Peer]
PublicKey = key3
AllowedIPs = 10.8.0.4/32`
	path := filepath.Join(dir, "multi.conf")
	writeConf(t, dir, "multi.conf", conf)

	parsed, err := ParseAWGConf(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Peers) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(parsed.Peers))
	}
	if parsed.Peers[2].Values["PublicKey"] != "key3" {
		t.Errorf("third peer key = %q", parsed.Peers[2].Values["PublicKey"])
	}
}

func TestParseAWGConf_CommentsAndWhitespace(t *testing.T) {
	dir := t.TempDir()
	conf := `# This is a comment
; Another comment

[Interface]
   PrivateKey = spaced_key
Address = 10.8.0.1/32

[Peer]
PublicKey = peer1`
	path := filepath.Join(dir, "commented.conf")
	writeConf(t, dir, "commented.conf", conf)

	parsed, err := ParseAWGConf(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Interface.Values["PrivateKey"] != "spaced_key" {
		t.Errorf("PrivateKey = %q (should trim whitespace)", parsed.Interface.Values["PrivateKey"])
	}
	if len(parsed.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(parsed.Peers))
	}
}
