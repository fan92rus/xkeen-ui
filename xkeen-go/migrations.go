package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fan92rus/xkeen-ui/internal/config"
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

// allMigrations is the ordered list of known one-time migrations.
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
	{
		name:        "002-awg-init-universal",
		description: "Replace custom S89awg-server + minimal S90awg with ONE universal full template (server + WARP clients + firewall + correct shebang)",
		run:         migrateAWGInitUniversal,
	},
	{
		name:        "003-awg-init-idempotent",
		description: "Rewrite S90awg with idempotent start/check (skip if interface already up, no duplicate firewall rules)",
		run:         migrateAWGInitUniversal,
	},
}

// startupMigrations is the list of tasks that run on EVERY server startup.
// Unlike one-time migrations (allMigrations), these are not tracked in the
// state file — they must be fully idempotent and cheap when there's nothing
// to do.
//
// Use cases:
//   - self-healing: recreate files/configs that may have been deleted
//   - reconciliation: ensure expected state is maintained
//
// Guidelines:
//   - must be safe to run on every startup (idempotent)
//   - must be FAST when nothing needs to change (stat a file, return)
//   - only print/log when an action is actually taken
// startupMigrations holds startup tasks that run on EVERY server launch.
// Populated by runServer() (which has the loaded config) via registerStartupMigrations().
// Kept empty here by design — callers wire closures over the real config.
var startupMigrations []migration

// registerStartupMigrations populates startupMigrations with tasks that need
// access to the loaded config. Called once from runServer() before runStartupMigrations().
func registerStartupMigrations(cfg *config.Config) {
	startupMigrations = []migration{
		{
			name:        "proxy-entware-reconcile",
			description: "Ensure xkeen -pr is on when proxy_entware is enabled in config",
			run:         func() error { return reconcileProxyEntware(cfg) },
		},
	}
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

// runStartupMigrations executes tasks that must run on EVERY server startup.
// These are not tracked in the state file — they must be idempotent and
// silent when there's nothing to do.
func runStartupMigrations() {
	for _, m := range startupMigrations {
		if err := m.run(); err != nil {
			fmt.Printf("Startup task %s: ⚠ %v\n", m.name, err)
		}
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
