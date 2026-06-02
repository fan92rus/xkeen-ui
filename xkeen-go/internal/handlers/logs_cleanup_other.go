//go:build !linux

package handlers

// killOrphanedTails is a no-op on non-Linux systems (no /proc filesystem).
func killOrphanedTails(_ []string) {}
