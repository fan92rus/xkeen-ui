// Package config provides configuration management for XKEEN-GO.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the application configuration.
type Config struct {
	// Port is the HTTP server listen port.
	Port int `json:"port"`

	// XrayConfigDir is the directory containing Xray configuration files.
	XrayConfigDir string `json:"xray_config_dir"`

	// XkeenBinary is the path or name of the xkeen binary.
	XkeenBinary string `json:"xkeen_binary"`

	// AllowedRoots defines the allowed directories for file operations.
	AllowedRoots []string `json:"allowed_roots"`

	// SessionSecret is used for session encryption.
	SessionSecret string `json:"session_secret"`

	// LogLevel defines the logging level (debug, info, warn, error).
	LogLevel string `json:"log_level"`

	// CORS configuration (disabled by default).
	CORS CORSConfig `json:"cors"`

	// Auth configuration.
	Auth AuthConfig `json:"auth"`
}

// CORSConfig holds CORS settings.
type CORSConfig struct {
	Enabled        bool     `json:"enabled"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	// Username for admin access.
	Username string `json:"username"`

	// PasswordHash is bcrypt hash of the password.
	PasswordHash string `json:"password_hash"`

	// SessionTimeout in hours (default: 24).
	SessionTimeout int `json:"session_timeout"`

	// MaxLoginAttempts before lockout (default: 5).
	MaxLoginAttempts int `json:"max_login_attempts"`

	// LockoutDuration in minutes (default: 5).
	LockoutDuration int `json:"lockout_duration"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:          8089,
		XrayConfigDir: "/opt/etc/xray/configs",
		XkeenBinary:   "xkeen",
		AllowedRoots: []string{
			"/opt/etc/xray",
			"/opt/etc/xkeen",
			"/opt/etc/mihomo",
			"/opt/var/log",
		},
		SessionSecret: "",
		LogLevel:      "info",
		CORS: CORSConfig{
			Enabled:        false,
			AllowedOrigins: []string{},
		},
		Auth: AuthConfig{
			Username:         "admin",
			PasswordHash:     "", // Will be generated on first run
			SessionTimeout:   24,
			MaxLoginAttempts: 5,
			LockoutDuration:  5,
		},
	}
}

// LoadConfig loads configuration from a JSON file at the specified path.
// If the file does not exist, returns DefaultConfig with an error.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := DefaultConfig()
	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// SaveConfig saves the configuration to a JSON file at the specified path.
func (c *Config) SaveConfig(path string) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}

	if c.XrayConfigDir == "" {
		return errors.New("xray_config_dir is required")
	}

	if c.XkeenBinary == "" {
		return errors.New("xkeen_binary is required")
	}

	if len(c.AllowedRoots) == 0 {
		return errors.New("allowed_roots must contain at least one directory")
	}

	// Validate allowed roots are absolute paths (Unix-style or Windows-style)
	for _, root := range c.AllowedRoots {
		// Accept Unix absolute paths (start with /) or Windows absolute paths
		if !filepath.IsAbs(root) && !isUnixAbs(root) {
			return fmt.Errorf("allowed_root must be an absolute path: %s", root)
		}
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if c.LogLevel != "" && !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log_level: %s (valid: debug, info, warn, error)", c.LogLevel)
	}

	// Validate auth settings
	if c.Auth.SessionTimeout < 1 {
		c.Auth.SessionTimeout = 24
	}
	if c.Auth.MaxLoginAttempts < 1 {
		c.Auth.MaxLoginAttempts = 5
	}
	if c.Auth.LockoutDuration < 1 {
		c.Auth.LockoutDuration = 5
	}

	return nil
}

// IsPathAllowed checks if a given path is within the allowed roots.
func (c *Config) IsPathAllowed(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, root := range c.AllowedRoots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		// Check if path is within this root
		rel, err := filepath.Rel(absRoot, absPath)
		if err == nil && !filepath.IsAbs(rel) && !startsWithDotDot(rel) {
			return true
		}
	}

	return false
}

// startsWithDotDot checks if a relative path starts with ".."
func startsWithDotDot(path string) bool {
	return len(path) >= 2 && path[0] == '.' && path[1] == '.'
}

// isUnixAbs checks if a path is a Unix-style absolute path
func isUnixAbs(path string) bool {
	return len(path) > 0 && path[0] == '/'
}
