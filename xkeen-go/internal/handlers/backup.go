package handlers

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// createBackupCore creates a timestamped backup of the specified file inside
// backupDir. The timestamp controls the backup filename suffix, allowing callers
// to choose the naming convention (current time vs. file modification time).
// Returns ("", nil) when the source file does not exist (nothing to back up).
//
// This is the shared implementation used by both ConfigHandler and
// SettingsHandler to avoid duplicated backup logic.
func createBackupCore(filePath, backupDir, timestamp string) (string, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil
	}

	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	baseName := filepath.Base(filePath)
	backupName := fmt.Sprintf("%s.%s.bak", baseName, timestamp)
	backupPath := filepath.Join(backupDir, backupName)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file for backup: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}

	return backupPath, nil
}

// cleanupOldBackupsCore removes old backups of filePath from backupDir, keeping
// only the most recent `keep` entries. Backups are identified by the naming
// convention "<basename>.*.bak" and sorted by modification time (newest first).
//
// Shared implementation extracted from ConfigHandler.cleanupOldBackups.
func cleanupOldBackupsCore(filePath, backupDir string, keep int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	baseName := filepath.Base(filePath)
	var backups []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, baseName+".") && strings.HasSuffix(name, ".bak") {
			backups = append(backups, entry)
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		infoI, errI := backups[i].Info()
		infoJ, errJ := backups[j].Info()
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	// Remove old backups beyond keep limit
	for i := keep; i < len(backups); i++ {
		backupPath := filepath.Join(backupDir, backups[i].Name())
		_ = os.Remove(backupPath)
	}
}
