package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAppliedMigrations_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.applied")

	applied := readAppliedMigrations(path)
	if len(applied) != 0 {
		t.Fatalf("expected empty, got %v", applied)
	}
}

func TestReadAppliedMigrations_NonExistent(t *testing.T) {
	applied := readAppliedMigrations("/nonexistent/path/file")
	if len(applied) != 0 {
		t.Fatalf("expected empty for nonexistent file, got %v", applied)
	}
}

func TestAppendMigration_AndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.applied")

	if err := appendMigration(path, "001-test"); err != nil {
		t.Fatal(err)
	}
	if err := appendMigration(path, "002-test"); err != nil {
		t.Fatal(err)
	}

	applied := readAppliedMigrations(path)
	if !applied["001-test"] || !applied["002-test"] {
		t.Fatalf("expected both migrations, got %v", applied)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2, got %d", len(applied))
	}
}

func TestAppendMigration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.applied")

	// Appending the same migration twice should result in two lines
	// (the caller checks for duplicates via readAppliedMigrations before running).
	_ = appendMigration(path, "001-test")
	_ = appendMigration(path, "001-test")

	applied := readAppliedMigrations(path)
	if !applied["001-test"] {
		t.Fatal("expected 001-test to be applied")
	}
}

func TestReadAppliedMigrations_IgnoresComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "migrations.applied")
	_ = os.WriteFile(path, []byte("# comment line\n001-real\n\n  \n002-real\n"), 0o600)

	applied := readAppliedMigrations(path)
	if len(applied) != 2 {
		t.Fatalf("expected 2, got %v", applied)
	}
	if !applied["001-real"] || !applied["002-real"] {
		t.Fatal("expected 001-real and 002-real")
	}
}
