package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// migrationsStateFile stores which migrations have been applied (one name per line).
const migrationsStateFile = "migrations.applied"

// migration is a one-time transformation that runs during install/update.
// Like database migrations: each runs exactly once, tracked by name.
type migration struct {
	name        string
	description string
	run         func() error
}

// allMigrations is the ordered list of known migrations.
// Append new migrations here. They run in order, each exactly once.
//
// Guidelines:
//   - name must be unique, format: NNN-short-description (e.g. "001-awg-s90")
//   - make migrations idempotent where possible (safe if partially applied)
//   - failed migrations are NOT marked as applied → retried next install
//   - log what you do via fmt.Printf so the user sees migration output
var allMigrations = []migration{
	{
		name:        "001-awg-s90-init",
		description: "Rename AWG init script to S90awg so all AWG interfaces auto-start at boot",
		run:         migrateAWGInitS90,
	},
}

// runMigrations executes all pending migrations, tracking state in a file
// so each runs exactly once (like DB migrations).
func runMigrations() {
	statePath := filepath.Join(installConfigDir, migrationsStateFile)
	applied := readAppliedMigrations(statePath)

	anyRan := false
	for _, m := range allMigrations {
		if applied[m.name] {
			continue
		}
		fmt.Printf("Migration %s: %s\n", m.name, m.description)
		if err := m.run(); err != nil {
			// Don't mark as applied — will retry on next install.
			fmt.Printf("  ⚠ FAILED: %v (will retry next install)\n", err)
			continue
		}
		if err := appendMigration(statePath, m.name); err != nil {
			fmt.Printf("  ⚠ Completed but failed to record: %v\n", err)
			continue
		}
		fmt.Printf("  ✓ Done\n")
		applied[m.name] = true
		anyRan = true
	}

	if !anyRan {
		count := len(applied)
		fmt.Printf("Migrations: all up to date (%d applied)\n", count)
	}
}

// readAppliedMigrations reads the state file and returns a set of applied names.
func readAppliedMigrations(path string) map[string]bool {
	result := make(map[string]bool)
	f, err := os.Open(path)
	if err != nil {
		return result
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name := scanner.Text()
		if name = strings.TrimSpace(name); name != "" && !strings.HasPrefix(name, "#") {
			result[name] = true
		}
	}
	return result
}

// appendMigration records a migration as applied.
func appendMigration(path, name string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create migrations dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open migrations state: %w", err)
	}
	defer func() { _ = f.Close() }()
	_, err = fmt.Fprintf(f, "%s\n", name)
	return err
}
