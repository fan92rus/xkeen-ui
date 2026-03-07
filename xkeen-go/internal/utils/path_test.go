package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewPathValidator(t *testing.T) {
	tests := []struct {
		name        string
		roots       []string
		wantErr     bool
		errContains string
	}{
		{
			name:    "single root",
			roots:   []string{"/opt/etc/xray"},
			wantErr: false,
		},
		{
			name:    "multiple roots",
			roots:   []string{"/opt/etc/xray", "/opt/etc/xkeen", "/opt/var/log"},
			wantErr: false,
		},
		{
			name:        "empty roots",
			roots:       []string{},
			wantErr:     true,
			errContains: "at least one allowed root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pv, err := NewPathValidator(tt.roots)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewPathValidator() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("NewPathValidator() unexpected error: %v", err)
			}
			if pv == nil {
				t.Error("NewPathValidator() returned nil validator")
			}
		})
	}
}

func TestPathValidator_Validate_ValidPaths(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	subDir := filepath.Join(tmpDir1, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	pv, err := NewPathValidator([]string{tmpDir1, tmpDir2})
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		wantPath string
	}{
		{
			name:     "root directory itself",
			path:     tmpDir1,
			wantPath: tmpDir1,
		},
		{
			name:     "subdirectory",
			path:     subDir,
			wantPath: subDir,
		},
		{
			name:     "path with trailing slash",
			path:     tmpDir1 + string(filepath.Separator),
			wantPath: tmpDir1,
		},
		{
			name:     "file within root",
			path:     filepath.Join(tmpDir1, "file.txt"),
			wantPath: filepath.Join(tmpDir1, "file.txt"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pv.Validate(tt.path)
			if err != nil {
				t.Errorf("Validate(%q) unexpected error: %v", tt.path, err)
				return
			}
			if got != tt.wantPath {
				t.Errorf("Validate(%q) = %q, want %q", tt.path, got, tt.wantPath)
			}
		})
	}
}

func TestPathValidator_Validate_TraversalAttempts(t *testing.T) {
	tmpDir := t.TempDir()

	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name:    "simple parent traversal",
			path:    filepath.Join(tmpDir, "..", "secret.txt"),
			wantErr: ErrPathTraversal,
		},
		{
			name:    "double parent traversal",
			path:    filepath.Join(tmpDir, "..", "..", "etc", "passwd"),
			wantErr: ErrPathTraversal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pv.Validate(tt.path)
			// On Windows, filepath.Join resolves .. to absolute path,
			// resulting in ErrPathOutsideRoot instead of ErrPathTraversal
			if runtime.GOOS == "windows" {
				if err != ErrPathTraversal && err != ErrPathOutsideRoot {
					t.Errorf("Validate(%q) error = %v, want %v or %v", tt.path, err, ErrPathTraversal, ErrPathOutsideRoot)
				}
			} else {
				if err != tt.wantErr {
					t.Errorf("Validate(%q) error = %v, want %v", tt.path, err, tt.wantErr)
				}
			}
		})
	}
}

func TestPathValidator_Validate_OutsideRoot(t *testing.T) {
	tmpDir := t.TempDir()
	otherDir := t.TempDir()

	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name:    "absolute path outside root",
			path:    otherDir,
			wantErr: ErrPathOutsideRoot,
		},
		{
			name:    "system file",
			path:    "/etc/passwd",
			wantErr: ErrPathOutsideRoot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pv.Validate(tt.path)
			if err != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, want %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestPathValidator_Validate_EmptyPath(t *testing.T) {
	pv, err := NewPathValidator([]string{"/tmp"})
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	_, err = pv.Validate("")
	if err != ErrEmptyPath {
		t.Errorf("Validate('') error = %v, want %v", err, ErrEmptyPath)
	}
}

func TestPathValidator_Validate_Symlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on Windows")
	}

	tmpDir := t.TempDir()
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0644); err != nil {
		t.Fatalf("failed to create outside file: %v", err)
	}

	symlinkPath := filepath.Join(tmpDir, "symlink")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	t.Run("symlinks not allowed (default)", func(t *testing.T) {
		pv, err := NewPathValidator([]string{tmpDir})
		if err != nil {
			t.Fatalf("failed to create validator: %v", err)
		}

		_, err = pv.Validate(symlinkPath)
		if err != ErrSymlinkDetected {
			t.Errorf("Validate(%q) error = %v, want %v", symlinkPath, err, ErrSymlinkDetected)
		}
	})

	t.Run("symlinks allowed", func(t *testing.T) {
		pv, err := NewPathValidator([]string{tmpDir}, WithSymlinks(true))
		if err != nil {
			t.Fatalf("failed to create validator: %v", err)
		}

		_, err = pv.Validate(symlinkPath)
		if err != ErrPathOutsideRoot {
			t.Errorf("Validate(%q) error = %v, want %v", symlinkPath, err, ErrPathOutsideRoot)
		}
	})
}

func TestPathValidator_IsAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	otherDir := t.TempDir()

	pv, err := NewPathValidator([]string{tmpDir})
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	tests := []struct {
		path      string
		wantAllow bool
	}{
		{tmpDir, true},
		{filepath.Join(tmpDir, "file.txt"), true},
		{otherDir, false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := pv.IsAllowed(tt.path)
			if got != tt.wantAllow {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.path, got, tt.wantAllow)
			}
		})
	}
}

func TestValidateFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{"valid filename", "file.txt", false},
		{"valid with extension", "config.json", false},
		{"empty filename", "", true},
		{"single dot", ".", true},
		{"double dot", "..", true},
		{"contains forward slash", "path/file.txt", true},
		{"contains backslash", "path\\file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFilename(tt.filename)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateFilename(%q) expected error, got nil", tt.filename)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateFilename(%q) unexpected error: %v", tt.filename, err)
			}
		})
	}
}

func TestCleanPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple/path", filepath.Join("simple", "path")},
		{"path/with/../parent", filepath.Join("path", "parent")},
		{"./relative", "relative"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := CleanPath(tt.input)
			if got != tt.expected {
				t.Errorf("CleanPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestJoinPath(t *testing.T) {
	tests := []struct {
		elements []string
		expected string
	}{
		{[]string{"path", "to", "file"}, filepath.Join("path", "to", "file")},
		{[]string{"/root", "subdir"}, filepath.Join("/root", "subdir")},
		{[]string{"single"}, "single"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := JoinPath(tt.elements...)
			if got != tt.expected {
				t.Errorf("JoinPath(%v) = %q, want %q", tt.elements, got, tt.expected)
			}
		})
	}
}
