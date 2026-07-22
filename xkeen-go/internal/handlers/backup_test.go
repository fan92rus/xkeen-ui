package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// === createBackupCore Tests ===

func TestCreateBackupCore_CreatesBackup(t *testing.T) {
	srcDir := t.TempDir()
	backupDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "config.json")
	if err := os.WriteFile(srcPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	backupPath, err := createBackupCore(srcPath, backupDir, "20250101-120000")
	if err != nil {
		t.Fatalf("createBackupCore: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected non-empty backup path")
	}

	name := filepath.Base(backupPath)
	if name != "config.json.20250101-120000.bak" {
		t.Errorf("backup name = %q, want config.json.20250101-120000.bak", name)
	}

	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("backup content = %q, want %q", string(data), "hello")
	}
}

func TestCreateBackupCore_NonexistentFile(t *testing.T) {
	backupDir := t.TempDir()

	backupPath, err := createBackupCore("/nonexistent/file.json", backupDir, "20250101-120000")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent file, got %v", err)
	}
	if backupPath != "" {
		t.Errorf("expected empty path for nonexistent file, got %q", backupPath)
	}
}

func TestCreateBackupCore_CreatesBackupDir(t *testing.T) {
	srcDir := t.TempDir()
	backupDir := filepath.Join(t.TempDir(), "nested", "backups")

	srcPath := filepath.Join(srcDir, "data.json")
	os.WriteFile(srcPath, []byte("{}"), 0o644)

	backupPath, err := createBackupCore(srcPath, backupDir, "20250101-120000")
	if err != nil {
		t.Fatalf("createBackupCore: %v", err)
	}

	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Error("backup directory should have been created")
	}
	if !strings.HasSuffix(backupPath, ".bak") {
		t.Errorf("backup path should end with .bak, got %q", backupPath)
	}
}

// === cleanupOldBackupsCore Tests ===

func TestCleanupOldBackupsCore_KeepsNewest(t *testing.T) {
	backupDir := t.TempDir()
	filePath := "/some/dir/app.json"

	// Create 7 backup files with increasing modification times.
	for i := 0; i < 7; i++ {
		name := filepath.Base(filePath) + ".2025010" + string(rune('1'+i)) + "-120000.bak"
		path := filepath.Join(backupDir, name)
		os.WriteFile(path, []byte("data"), 0o600)
		// Stagger mtime so ordering is deterministic.
		mtime := time.Date(2025, 1, i+1, 12, 0, 0, 0, time.UTC)
		os.Chtimes(path, mtime, mtime)
	}

	cleanupOldBackupsCore(filePath, backupDir, 5)

	entries, _ := os.ReadDir(backupDir)
	if len(entries) != 5 {
		t.Errorf("expected 5 backups after cleanup, got %d", len(entries))
	}
}

func TestCleanupOldBackupsCore_OnlyMatchingBaseName(t *testing.T) {
	backupDir := t.TempDir()
	filePath := "/some/dir/app.json"

	// Matching backup
	os.WriteFile(filepath.Join(backupDir, "app.json.20250101-120000.bak"), []byte("x"), 0o600)
	// Non-matching (different base name) — must NOT be removed
	os.WriteFile(filepath.Join(backupDir, "other.json.20250101-120000.bak"), []byte("x"), 0o600)
	// Non-matching (not .bak)
	os.WriteFile(filepath.Join(backupDir, "app.json.20250101-120000.tmp"), []byte("x"), 0o600)

	cleanupOldBackupsCore(filePath, backupDir, 0)

	entries, _ := os.ReadDir(backupDir)
	// app.json backup removed (keep 0), other files remain
	if len(entries) != 2 {
		t.Errorf("expected 2 remaining files (non-matching), got %d", len(entries))
	}
}

func TestCleanupOldBackupsCore_NonexistentDir(t *testing.T) {
	// Should not panic / error on missing directory.
	cleanupOldBackupsCore("/some/file.json", "/nonexistent/backup/dir", 5)
}
