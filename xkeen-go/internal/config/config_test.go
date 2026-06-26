package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// validConfig returns a Config that passes Validate().
func validConfig() *Config {
	return &Config{
		Port:            8089,
		Mode:            "xray",
		XrayConfigDir:   "/opt/etc/xray/configs",
		XkeenBinary:     "xkeen",
		MihomoConfigDir: "/opt/etc/mihomo",
		MihomoBinary:    "mihomo",
		AllowedRoots:    []string{"/opt/etc/xray", "/opt/etc/xkeen"},
		LogLevel:        "info",
		Auth: AuthConfig{
			SessionTimeout:   24,
			MaxLoginAttempts: 5,
			LockoutDuration:  5,
		},
	}
}

// --- DefaultConfig ---

func TestDefaultConfig_ReturnsValidConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Port != 8089 {
		t.Errorf("Port = %d, want 8089", cfg.Port)
	}
	if cfg.Mode != "xray" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "xray")
	}
	if cfg.XrayConfigDir != "/opt/etc/xray/configs" {
		t.Errorf("XrayConfigDir = %q, want %q", cfg.XrayConfigDir, "/opt/etc/xray/configs")
	}
	if cfg.XkeenBinary != "xkeen" {
		t.Errorf("XkeenBinary = %q, want %q", cfg.XkeenBinary, "xkeen")
	}
	if cfg.MihomoConfigDir != "/opt/etc/mihomo" {
		t.Errorf("MihomoConfigDir = %q, want %q", cfg.MihomoConfigDir, "/opt/etc/mihomo")
	}
	if cfg.MihomoBinary != "mihomo" {
		t.Errorf("MihomoBinary = %q, want %q", cfg.MihomoBinary, "mihomo")
	}
	if cfg.AWGConfigDir != "/opt/etc/awg" {
		t.Errorf("AWGConfigDir = %q, want %q", cfg.AWGConfigDir, "/opt/etc/awg")
	}
	if len(cfg.AllowedRoots) != 5 {
		t.Fatalf("len(AllowedRoots) = %d, want 5", len(cfg.AllowedRoots))
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.Auth.SessionTimeout != 24 {
		t.Errorf("Auth.SessionTimeout = %d, want 24", cfg.Auth.SessionTimeout)
	}
	if cfg.Auth.MaxLoginAttempts != 5 {
		t.Errorf("Auth.MaxLoginAttempts = %d, want 5", cfg.Auth.MaxLoginAttempts)
	}
	if cfg.Auth.LockoutDuration != 5 {
		t.Errorf("Auth.LockoutDuration = %d, want 5", cfg.Auth.LockoutDuration)
	}
	if cfg.SessionSecret != "" {
		t.Errorf("SessionSecret = %q, want empty", cfg.SessionSecret)
	}
	if cfg.CORS.Enabled {
		t.Error("CORS.Enabled = true, want false")
	}
}

func TestDefaultConfig_PassesValidate(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig.Validate() returned error: %v", err)
	}
}

// --- Validate: Port ---

func TestValidate_RejectsInvalidPorts(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too large", 65536},
		{"way too large", 70000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Port = tt.port
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() with port %d should return error", tt.port)
			}
			if err.Error() != "port must be between 1 and 65535" {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidate_AcceptsValidPorts(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"one", 1},
		{"http", 80},
		{"https", 443},
		{"custom", 8080},
		{"max", 65535},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Port = tt.port
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() with port %d returned error: %v", tt.port, err)
			}
		})
	}
}

// --- Validate: Required fields ---

func TestValidate_RequiresXrayConfigDir(t *testing.T) {
	cfg := validConfig()
	cfg.XrayConfigDir = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty XrayConfigDir")
	}
	if err.Error() != "xray_config_dir is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_RequiresXkeenBinary(t *testing.T) {
	cfg := validConfig()
	cfg.XkeenBinary = ""
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty XkeenBinary")
	}
	if err.Error() != "xkeen_binary is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_RequiresAllowedRoots(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty AllowedRoots")
	}
	if err.Error() != "allowed_roots must contain at least one directory" {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Validate: AllowedRoots must be absolute ---

func TestValidate_RejectsRelativeAllowedRoot(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{"relative/path"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for relative AllowedRoot")
	}
}

func TestValidate_AcceptsAbsoluteAllowedRoots(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{"/opt/etc/xray", "/opt/etc/xkeen"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with absolute roots returned error: %v", err)
	}
}

// --- Validate: LogLevel ---

func TestValidate_AcceptsValidLogLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			cfg := validConfig()
			cfg.LogLevel = level
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() with log level %q returned error: %v", level, err)
			}
		})
	}
}

func TestValidate_RejectsInvalidLogLevel(t *testing.T) {
	cfg := validConfig()
	cfg.LogLevel = "verbose"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestValidate_AcceptsEmptyLogLevel(t *testing.T) {
	cfg := validConfig()
	cfg.LogLevel = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with empty LogLevel should pass (skipped check): %v", err)
	}
}

// --- Validate: Mode defaults ---

func TestValidate_DefaultsInvalidModeToXray(t *testing.T) {
	cfg := validConfig()
	cfg.Mode = "invalid"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if cfg.Mode != "xray" {
		t.Errorf("Mode = %q, want %q after Validate", cfg.Mode, "xray")
	}
}

func TestValidate_AcceptsXrayMode(t *testing.T) {
	cfg := validConfig()
	cfg.Mode = "xray"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with xray mode returned error: %v", err)
	}
}

func TestValidate_AcceptsMihomoMode(t *testing.T) {
	cfg := validConfig()
	cfg.Mode = "mihomo"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with mihomo mode returned error: %v", err)
	}
}

// --- Validate: Auth defaults ---

func TestValidate_DefaultsAuthSessionTimeout(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.SessionTimeout = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if cfg.Auth.SessionTimeout != 24 {
		t.Errorf("Auth.SessionTimeout = %d, want 24", cfg.Auth.SessionTimeout)
	}
}

func TestValidate_DefaultsAuthMaxLoginAttempts(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.MaxLoginAttempts = -1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if cfg.Auth.MaxLoginAttempts != 5 {
		t.Errorf("Auth.MaxLoginAttempts = %d, want 5", cfg.Auth.MaxLoginAttempts)
	}
}

func TestValidate_DefaultsAuthLockoutDuration(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.LockoutDuration = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
	if cfg.Auth.LockoutDuration != 5 {
		t.Errorf("Auth.LockoutDuration = %d, want 5", cfg.Auth.LockoutDuration)
	}
}

// --- LoadConfig ---

func TestLoadConfig_NonExistentFile_ReturnsDefaultWithError(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if cfg == nil {
		t.Fatal("expected non-nil config (default)")
	}
	if cfg.Port != 8089 {
		t.Errorf("Port = %d, want default 8089", cfg.Port)
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	input := map[string]interface{}{
		"port":            9090,
		"mode":            "mihomo",
		"xray_config_dir": "/opt/etc/xray",
		"xkeen_binary":    "xkeen",
		"allowed_roots":   []string{"/opt/etc/xray"},
		"log_level":       "debug",
	}
	data, _ := json.Marshal(input)
	os.WriteFile(path, data, 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.Mode != "mihomo" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "mihomo")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("{invalid json}"), 0644)

	cfg, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if cfg != nil {
		t.Error("expected nil config for invalid JSON")
	}
}

func TestLoadConfig_ValidJSON_InvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Valid JSON but port is out of range
	input := map[string]interface{}{
		"port":            0,
		"xray_config_dir": "/opt/etc/xray",
		"xkeen_binary":    "xkeen",
		"allowed_roots":   []string{"/opt/etc/xray"},
	}
	data, _ := json.Marshal(input)
	os.WriteFile(path, data, 0644)

	cfg, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid config values")
	}
	if cfg != nil {
		t.Error("expected nil config for validation failure")
	}
}

func TestLoadConfig_MergesOntoDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Only override port — everything else should come from defaults
	input := map[string]interface{}{
		"port": 1234,
	}
	data, _ := json.Marshal(input)
	os.WriteFile(path, data, 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}
	if cfg.Port != 1234 {
		t.Errorf("Port = %d, want 1234", cfg.Port)
	}
	// Defaults should fill in
	if cfg.XkeenBinary != "xkeen" {
		t.Errorf("XkeenBinary = %q, want default %q", cfg.XkeenBinary, "xkeen")
	}
	if cfg.Auth.SessionTimeout != 24 {
		t.Errorf("Auth.SessionTimeout = %d, want default 24", cfg.Auth.SessionTimeout)
	}
}

// --- SaveConfig ---

func TestSaveConfig_WritesValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.json")

	cfg := validConfig()
	if err := cfg.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig() returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal saved config: %v", err)
	}
	if loaded.Port != cfg.Port {
		t.Errorf("loaded Port = %d, want %d", loaded.Port, cfg.Port)
	}
	if loaded.Mode != cfg.Mode {
		t.Errorf("loaded Mode = %q, want %q", loaded.Mode, cfg.Mode)
	}

	// Verify file permissions are 0600 (owner read/write only) on Unix systems.
	// On Windows, the permission bits may not be enforced the same way.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat saved file: %v", err)
	}
	perms := fi.Mode().Perm()
	if runtime.GOOS != "windows" && perms != 0600 {
		t.Errorf("saved config has permissions %o, want 0600", perms)
	}
}

func TestSaveConfig_InvalidConfig_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := &Config{Port: 0} // invalid
	err := cfg.SaveConfig(path)
	if err == nil {
		t.Fatal("expected error saving invalid config")
	}

	// File should not have been written
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("expected file to not exist after failed save")
	}
}

func TestSaveConfig_CreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "config.json")

	cfg := validConfig()
	if err := cfg.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig() returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// --- Round-trip ---

func TestRoundTrip_SaveThenLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := validConfig()
	original.Port = 9999
	original.Mode = "mihomo"
	original.LogLevel = "debug"
	original.Auth.SessionTimeout = 48
	original.Auth.MaxLoginAttempts = 10
	original.Auth.LockoutDuration = 15
	original.Auth.ForcePasswordChange = true
	original.CORS.Enabled = true
	original.CORS.AllowedOrigins = []string{"https://example.com"}

	if err := original.SaveConfig(path); err != nil {
		t.Fatalf("SaveConfig() returned error: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() returned error: %v", err)
	}

	if loaded.Port != original.Port {
		t.Errorf("Port = %d, want %d", loaded.Port, original.Port)
	}
	if loaded.Mode != original.Mode {
		t.Errorf("Mode = %q, want %q", loaded.Mode, original.Mode)
	}
	if loaded.LogLevel != original.LogLevel {
		t.Errorf("LogLevel = %q, want %q", loaded.LogLevel, original.LogLevel)
	}
	if loaded.Auth.SessionTimeout != original.Auth.SessionTimeout {
		t.Errorf("Auth.SessionTimeout = %d, want %d", loaded.Auth.SessionTimeout, original.Auth.SessionTimeout)
	}
	if loaded.Auth.MaxLoginAttempts != original.Auth.MaxLoginAttempts {
		t.Errorf("Auth.MaxLoginAttempts = %d, want %d", loaded.Auth.MaxLoginAttempts, original.Auth.MaxLoginAttempts)
	}
	if loaded.Auth.LockoutDuration != original.Auth.LockoutDuration {
		t.Errorf("Auth.LockoutDuration = %d, want %d", loaded.Auth.LockoutDuration, original.Auth.LockoutDuration)
	}
	if loaded.Auth.ForcePasswordChange != original.Auth.ForcePasswordChange {
		t.Errorf("Auth.ForcePasswordChange = %v, want %v", loaded.Auth.ForcePasswordChange, original.Auth.ForcePasswordChange)
	}
	if loaded.CORS.Enabled != original.CORS.Enabled {
		t.Errorf("CORS.Enabled = %v, want %v", loaded.CORS.Enabled, original.CORS.Enabled)
	}
	if len(loaded.CORS.AllowedOrigins) != 1 || loaded.CORS.AllowedOrigins[0] != "https://example.com" {
		t.Errorf("CORS.AllowedOrigins = %v, want [https://example.com]", loaded.CORS.AllowedOrigins)
	}
	if loaded.XrayConfigDir != original.XrayConfigDir {
		t.Errorf("XrayConfigDir = %q, want %q", loaded.XrayConfigDir, original.XrayConfigDir)
	}
	if loaded.XkeenBinary != original.XkeenBinary {
		t.Errorf("XkeenBinary = %q, want %q", loaded.XkeenBinary, original.XkeenBinary)
	}
}

// --- IsPathAllowed ---

func TestIsPathAllowed_TrueForPathWithinRoot(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{"/opt/etc/xray"}

	tests := []struct {
		name string
		path string
	}{
		{"direct child", "/opt/etc/xray/config.json"},
		{"nested", "/opt/etc/xray/configs/03_inbounds.json"},
		{"deeply nested", "/opt/etc/xray/a/b/c/d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !cfg.IsPathAllowed(tt.path) {
				t.Errorf("IsPathAllowed(%q) = false, want true", tt.path)
			}
		})
	}
}

func TestIsPathAllowed_FalseForPathOutsideRoot(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{"/opt/etc/xray"}

	tests := []struct {
		name string
		path string
	}{
		{"sibling", "/opt/etc/xkeen/config.json"},
		{"parent", "/opt/etc"},
		{"unrelated", "/tmp/evil"},
		{"traversal attempt", "/opt/etc/xray/../../etc/passwd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if cfg.IsPathAllowed(tt.path) {
				t.Errorf("IsPathAllowed(%q) = true, want false", tt.path)
			}
		})
	}
}

func TestIsPathAllowed_MultipleRoots(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{"/opt/etc/xray", "/opt/var/log"}

	if !cfg.IsPathAllowed("/opt/etc/xray/config.json") {
		t.Error("expected /opt/etc/xray/config.json to be allowed")
	}
	if !cfg.IsPathAllowed("/opt/var/log/xray/access.log") {
		t.Error("expected /opt/var/log/xray/access.log to be allowed")
	}
	if cfg.IsPathAllowed("/etc/passwd") {
		t.Error("expected /etc/passwd to be disallowed")
	}
}

func TestIsPathAllowed_ExactlyRoot(t *testing.T) {
	cfg := validConfig()
	cfg.AllowedRoots = []string{"/opt/etc/xray"}

	// The root itself is technically "within" itself via filepath.Rel returning "."
	if !cfg.IsPathAllowed("/opt/etc/xray") {
		t.Error("root path itself should be allowed")
	}
}
