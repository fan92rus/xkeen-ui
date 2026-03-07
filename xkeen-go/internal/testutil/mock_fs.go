// Package testutil provides mock infrastructure for testing XKEEN-GO
// without physical device dependencies.
package testutil

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
)

// MockFS implements filesystem operations in memory for testing purposes.
// It simulates the Keenetic router filesystem without requiring actual hardware.
type MockFS struct {
	mu    sync.RWMutex
	files map[string][]byte
	dirs  map[string]bool
}

// NewMockFS creates a new mock filesystem with basic directory structure
// that mimics a Keenetic router environment.
func NewMockFS() *MockFS {
	return &MockFS{
		files: make(map[string][]byte),
		dirs: map[string]bool{
			"/":                    true,
			"/opt":                 true,
			"/opt/etc":             true,
			"/opt/etc/xray":        true,
			"/opt/etc/xray/configs": true,
			"/opt/etc/xkeen":       true,
			"/opt/etc/xkeen-go":    true,
			"/opt/var":             true,
			"/opt/var/log":         true,
			"/opt/var/log/xray":    true,
			"/opt/bin":             true,
			"/opt/tmp":             true,
		},
	}
}

// ReadFile reads the content of a file from the mock filesystem.
// Returns an error if the file does not exist.
func (m *MockFS) ReadFile(path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedPath := normalizePath(path)
	data, ok := m.files[normalizedPath]
	if !ok {
		return nil, errors.New("file not found: " + normalizedPath)
	}
	return data, nil
}

// WriteFile writes data to a file in the mock filesystem.
// Creates parent directories if they don't exist.
func (m *MockFS) WriteFile(path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalizedPath := normalizePath(path)

	// Ensure parent directory exists
	parentDir := filepath.Dir(normalizedPath)
	if parentDir != "/" && parentDir != "" {
		m.ensureDirLocked(parentDir)
	}

	m.files[normalizedPath] = make([]byte, len(data))
	copy(m.files[normalizedPath], data)
	return nil
}

// Delete removes a file or empty directory from the mock filesystem.
func (m *MockFS) Delete(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalizedPath := normalizePath(path)
	delete(m.files, normalizedPath)
	delete(m.dirs, normalizedPath)
	return nil
}

// ListDir returns all files and directories directly within the specified path.
func (m *MockFS) ListDir(path string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedPath := normalizePath(path)

	if !m.dirs[normalizedPath] {
		return nil, errors.New("directory not found: " + normalizedPath)
	}

	var entries []string
	prefix := normalizedPath
	if normalizedPath != "/" {
		prefix = normalizedPath + "/"
	}

	// Find files in this directory
	for f := range m.files {
		if strings.HasPrefix(f, prefix) {
			// Get just the filename (not full path)
			relative := strings.TrimPrefix(f, prefix)
			if !strings.Contains(relative, "/") {
				entries = append(entries, relative)
			}
		}
	}

	// Find subdirectories
	for d := range m.dirs {
		if d != normalizedPath && strings.HasPrefix(d, prefix) {
			relative := strings.TrimPrefix(d, prefix)
			if !strings.Contains(relative, "/") {
				entries = append(entries, relative+"/")
			}
		}
	}

	return entries, nil
}

// Mkdir creates a new directory in the mock filesystem.
// Parent directories are created if they don't exist.
func (m *MockFS) Mkdir(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalizedPath := normalizePath(path)
	m.ensureDirLocked(normalizedPath)
	return nil
}

// Exists checks if a file or directory exists in the mock filesystem.
func (m *MockFS) Exists(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedPath := normalizePath(path)
	_, fileExists := m.files[normalizedPath]
	_, dirExists := m.dirs[normalizedPath]
	return fileExists || dirExists
}

// IsDir checks if the path is a directory.
func (m *MockFS) IsDir(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedPath := normalizePath(path)
	return m.dirs[normalizedPath]
}

// CopyFile copies a file from src to dst.
func (m *MockFS) CopyFile(src, dst string) error {
	data, err := m.ReadFile(src)
	if err != nil {
		return err
	}
	return m.WriteFile(dst, data)
}

// MoveFile moves a file from src to dst.
func (m *MockFS) MoveFile(src, dst string) error {
	data, err := m.ReadFile(src)
	if err != nil {
		return err
	}
	if err := m.WriteFile(dst, data); err != nil {
		return err
	}
	return m.Delete(src)
}

// Stat returns information about a file.
func (m *MockFS) Stat(path string) (FileInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedPath := normalizePath(path)

	if data, ok := m.files[normalizedPath]; ok {
		return FileInfo{
			Name:  filepath.Base(normalizedPath),
			Size:  int64(len(data)),
			IsDir: false,
		}, nil
	}

	if m.dirs[normalizedPath] {
		return FileInfo{
			Name:  filepath.Base(normalizedPath),
			Size:  0,
			IsDir: true,
		}, nil
	}

	return FileInfo{}, errors.New("file not found: " + normalizedPath)
}

// Walk traverses the filesystem tree starting from root.
func (m *MockFS) Walk(root string, walkFn func(path string, info FileInfo) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	normalizedRoot := normalizePath(root)

	// Walk directories
	for d := range m.dirs {
		if strings.HasPrefix(d, normalizedRoot) || d == normalizedRoot {
			info := FileInfo{
				Name:  filepath.Base(d),
				Size:  0,
				IsDir: true,
			}
			if err := walkFn(d, info); err != nil {
				return err
			}
		}
	}

	// Walk files
	for f, data := range m.files {
		if strings.HasPrefix(f, normalizedRoot) {
			info := FileInfo{
				Name:  filepath.Base(f),
				Size:  int64(len(data)),
				IsDir: false,
			}
			if err := walkFn(f, info); err != nil {
				return err
			}
		}
	}

	return nil
}

// Clear removes all files and directories (except root).
func (m *MockFS) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files = make(map[string][]byte)
	m.dirs = map[string]bool{
		"/": true,
	}
}

// GetAllFiles returns all file paths in the filesystem.
func (m *MockFS) GetAllFiles() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	files := make([]string, 0, len(m.files))
	for f := range m.files {
		files = append(files, f)
	}
	return files
}

// GetAllDirs returns all directory paths in the filesystem.
func (m *MockFS) GetAllDirs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dirs := make([]string, 0, len(m.dirs))
	for d := range m.dirs {
		dirs = append(dirs, d)
	}
	return dirs
}

// ensureDirLocked creates directory and all parent directories.
// Must be called with lock held.
func (m *MockFS) ensureDirLocked(path string) {
	if path == "/" || path == "" {
		return
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current += "/" + part
		m.dirs[current] = true
	}
}

// FileInfo represents file metadata.
type FileInfo struct {
	Name  string
	Size  int64
	IsDir bool
}

// SeedTestData populates the mock filesystem with sample xray configs
// and other test data that mimics a real Keenetic router setup.
func (m *MockFS) SeedTestData() {
	// Create necessary directories
	m.Mkdir("/opt/etc/xray/configs")
	m.Mkdir("/opt/var/log/xray")
	m.Mkdir("/opt/etc/xkeen-go")

	// Sample inbound configuration
	m.WriteFile("/opt/etc/xray/configs/03_inbounds.json", []byte(`{
    "inbounds": [
        {
            "tag": "tun-in",
            "type": "tun",
            "settings": {
                "name": "tun0",
                "mtu": 1500,
                "gateway": "10.0.0.1",
                "dns": ["8.8.8.8", "1.1.1.1"]
            },
            "sniffing": {
                "enabled": true,
                "destOverride": ["http", "tls"]
            }
        }
    ]
}`))

	// Sample outbound configuration
	m.WriteFile("/opt/etc/xray/configs/04_outbounds.json", []byte(`{
    "outbounds": [
        {
            "tag": "proxy",
            "type": "vless",
            "settings": {
                "vnext": [
                    {
                        "address": "example.com",
                        "port": 443,
                        "users": [
                            {
                                "id": "uuid-here",
                                "encryption": "none"
                            }
                        ]
                    }
                ]
            },
            "streamSettings": {
                "network": "tcp",
                "security": "tls"
            }
        },
        {
            "tag": "direct",
            "type": "freedom",
            "settings": {}
        },
        {
            "tag": "block",
            "type": "blackhole",
            "settings": {}
        }
    ]
}`))

	// Sample routing configuration
	m.WriteFile("/opt/etc/xray/configs/05_routing.json", []byte(`{
    "routing": {
        "domainStrategy": "IPIfNonMatch",
        "rules": [
            {
                "type": "field",
                "outboundTag": "block",
                "domain": ["geosite:category-ads-all"]
            },
            {
                "type": "field",
                "outboundTag": "proxy",
                "domain": ["geosite:google", "geosite:youtube"]
            },
            {
                "type": "field",
                "outboundTag": "direct",
                "domain": ["geosite:ru"]
            },
            {
                "type": "field",
                "outboundTag": "proxy",
                "network": "tcp,udp"
            }
        ]
    }
}`))

	// Sample DNS configuration
	m.WriteFile("/opt/etc/xray/configs/02_dns.json", []byte(`{
    "dns": {
        "servers": [
            {
                "address": "8.8.8.8",
                "domains": ["geosite:geolocation-!ru"]
            },
            {
                "address": "77.88.8.8",
                "domains": ["geosite:ru"]
            }
        ]
    }
}`))

	// Sample log file
	m.WriteFile("/opt/var/log/xray/access.log", []byte(`2026-03-03 10:00:00 [Info] accepted tcp:google.com:443
2026-03-03 10:00:01 [Info] accepted tcp:youtube.com:443
2026-03-03 10:00:02 [Warning] blocked ads tracker
`))

	m.WriteFile("/opt/var/log/xray/error.log", []byte(`2026-03-03 09:00:00 [Error] connection timeout
`))

	// Sample xkeen-go auth file
	m.WriteFile("/opt/etc/xkeen-go/auth.json", []byte(`{
    "username": "admin",
    "password_hash": "$2a$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/X4.qO.1BoWBPfGKWe",
    "created_at": "2026-03-01T00:00:00Z"
}`))

	// Sample xkeen-go config
	m.WriteFile("/opt/etc/xkeen-go/config.json", []byte(`{
    "port": 8089,
    "session_timeout": 3600,
    "max_login_attempts": 5,
    "lockout_duration": 300
}`))
}

// normalizePath normalizes a file path by cleaning it and ensuring it starts with /.
func normalizePath(path string) string {
	path = filepath.Clean(path)
	path = strings.ReplaceAll(path, "\\", "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
