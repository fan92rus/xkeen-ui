package subscription

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanAWGConfigs_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o755)

	store, _ := NewStore(filepath.Join(dir, "subs.json"))
	configs, err := store.ScanAWGConfigs(awgDir)
	if err != nil {
		t.Fatalf("ScanAWGConfigs: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs in empty dir, got %d", len(configs))
	}
}

func TestScanAWGConfigs_SingleConfig(t *testing.T) {
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o755)

	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`

	os.WriteFile(filepath.Join(awgDir, "warp.conf"), []byte(confContent), 0o644)

	store, _ := NewStore(filepath.Join(dir, "subs.json"))
	configs, err := store.ScanAWGConfigs(awgDir)
	if err != nil {
		t.Fatalf("ScanAWGConfigs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Name != "warp" {
		t.Errorf("expected name 'warp', got %q", configs[0].Name)
	}
	if configs[0].Mark != 100 {
		t.Errorf("expected mark 100, got %d", configs[0].Mark)
	}
}

func TestScanAWGConfigs_IncrementalMarks(t *testing.T) {
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o755)

	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`

	os.WriteFile(filepath.Join(awgDir, "warp1.conf"), []byte(confContent), 0o644)
	os.WriteFile(filepath.Join(awgDir, "warp2.conf"), []byte(confContent), 0o644)
	os.WriteFile(filepath.Join(awgDir, "warp3.conf"), []byte(confContent), 0o644)

	store, _ := NewStore(filepath.Join(dir, "subs.json"))
	configs, err := store.ScanAWGConfigs(awgDir)
	if err != nil {
		t.Fatalf("ScanAWGConfigs: %v", err)
	}
	if len(configs) != 3 {
		t.Fatalf("expected 3 configs, got %d", len(configs))
	}
	// Marks should be allocated sequentially: 100, 101, 102 (sorted by name)
	marks := make(map[int]bool)
	for _, c := range configs {
		if marks[c.Mark] {
			t.Errorf("duplicate mark %d for %s", c.Mark, c.Name)
		}
		marks[c.Mark] = true
	}
	if configs[0].Mark != 100 || configs[1].Mark != 101 || configs[2].Mark != 102 {
		t.Errorf("expected marks 100,101,102 but got %d,%d,%d",
			configs[0].Mark, configs[1].Mark, configs[2].Mark)
	}
}

func TestScanAWGConfigs_PreservesExistingMarks(t *testing.T) {
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o755)

	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`

	os.WriteFile(filepath.Join(awgDir, "warp.conf"), []byte(confContent), 0o644)

	store, _ := NewStore(filepath.Join(dir, "subs.json"))
	configs, _ := store.ScanAWGConfigs(awgDir)

	// Second scan should preserve marks
	configs2, err := store.ScanAWGConfigs(awgDir)
	if err != nil {
		t.Fatalf("second ScanAWGConfigs: %v", err)
	}
	if len(configs2) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs2))
	}
	if configs2[0].Mark != configs[0].Mark {
		t.Errorf("mark changed between scans: %d -> %d", configs[0].Mark, configs2[0].Mark)
	}
}

func TestGenerateAWGProxies(t *testing.T) {
	configs := []AWGConfig{
		{Name: "warp", Mark: 100},
	}

	proxies := GenerateAWGProxies(configs)
	if len(proxies) != 1 {
		t.Fatalf("expected 1 proxy, got %d", len(proxies))
	}

	p := proxies[0]
	if p.Protocol != "awg" {
		t.Errorf("expected protocol awg, got %q", p.Protocol)
	}
	if p.Tag != "awg-warp" {
		t.Errorf("expected tag awg-warp, got %q", p.Tag)
	}
	if p.SubscriptionID != "__awg__" {
		t.Errorf("expected subscription_id __awg__, got %q", p.SubscriptionID)
	}
}

func TestGenerateAWGProxies_MultipleConfigs(t *testing.T) {
	configs := []AWGConfig{
		{Name: "warp1", Mark: 100},
		{Name: "warp2", Mark: 101},
	}

	proxies := GenerateAWGProxies(configs)
	if len(proxies) != 2 {
		t.Fatalf("expected 2 proxies, got %d", len(proxies))
	}

	// Tags are sorted by name
	if proxies[0].Tag != "awg-warp1" {
		t.Errorf("expected tag awg-warp1, got %q", proxies[0].Tag)
	}
	if proxies[1].Tag != "awg-warp2" {
		t.Errorf("expected tag awg-warp2, got %q", proxies[1].Tag)
	}

	// Each proxy should have outbound data
	for i, p := range proxies {
		if p.Outbound == nil {
			t.Fatalf("proxy %d has nil outbound", i)
		}
	}
}

func TestRemoveAWGConfig(t *testing.T) {
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o755)

	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`
	os.WriteFile(filepath.Join(awgDir, "warp1.conf"), []byte(confContent), 0o644)
	os.WriteFile(filepath.Join(awgDir, "warp2.conf"), []byte(confContent), 0o644)

	store, _ := NewStore(filepath.Join(dir, "subs.json"))
	store.ScanAWGConfigs(awgDir)

	// Verify both configs exist
	cfg1, ok := store.GetAWGConfig("warp1")
	if !ok {
		t.Fatal("expected warp1 to exist")
	}
	_ = cfg1
	cfg2, ok := store.GetAWGConfig("warp2")
	if !ok {
		t.Fatal("expected warp2 to exist")
	}
	_ = cfg2

	// Remove warp1
	store.RemoveAWGConfig("warp1")

	// Verify removed
	if _, ok := store.GetAWGConfig("warp1"); ok {
		t.Error("expected warp1 to be removed")
	}
	// Verify warp2 still exists
	if _, ok := store.GetAWGConfig("warp2"); !ok {
		t.Error("expected warp2 to still exist")
	}
}

func TestListAWGConfigs(t *testing.T) {
	dir := t.TempDir()
	awgDir := filepath.Join(dir, "awg")
	os.MkdirAll(awgDir, 0o755)

	confContent := `[Interface]
PrivateKey = aA1bB2cC3dD4eE5fF6gG7hH8iI9jJ0kK1lL2mM3nN4oO=
Address = 10.0.0.2/32

[Peer]
PublicKey = pP0oO1iI2uU3yY4tT5rR6eE7wW8qQ9zZ0xX1cC2vV3bB4nN5mM=
AllowedIPs = 0.0.0.0/0
Endpoint = 162.159.192.192:2408
`
	os.WriteFile(filepath.Join(awgDir, "warp.conf"), []byte(confContent), 0o644)

	store, _ := NewStore(filepath.Join(dir, "subs.json"))
	store.ScanAWGConfigs(awgDir)

	list := store.ListAWGConfigs()
	if len(list) != 1 {
		t.Fatalf("expected 1 config in list, got %d", len(list))
	}
	if list[0].Name != "warp" {
		t.Errorf("expected name warp, got %q", list[0].Name)
	}
}
