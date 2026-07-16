//go:build linux

package handlers

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// killOrphanedTails scans /proc for tail -F processes watching our log files
// and kills them. This handles leftover processes from previous xkeen-go runs
// that were killed without graceful shutdown.
func killOrphanedTails(logFiles []string) {
	if _, err := os.Stat("/proc"); err != nil {
		return
	}

	ourFiles := make(map[string]bool)
	for _, f := range logFiles {
		ourFiles[f] = true
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}

	killed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}

		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline")) //nolint:gocritic // /proc is a valid absolute path prefix
		if err != nil {
			continue
		}

		// cmdline is null-separated: ["tail", "-F", "/opt/var/log/xray/access.log"]
		parts := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
		if len(parts) < 3 {
			continue
		}

		if filepath.Base(parts[0]) != "tail" {
			continue
		}

		// Check if watching any of our files (args after "tail" and flags)
		shouldKill := false
		for i := 1; i < len(parts); i++ {
			if ourFiles[parts[i]] {
				shouldKill = true
				break
			}
		}

		if shouldKill {
			log.Printf("[logs] Killing orphaned tail process: pid=%d", pid)
			_ = syscall.Kill(pid, syscall.SIGTERM)
			killed++
		}
	}

	if killed > 0 {
		log.Printf("[logs] Killed %d orphaned tail process(es)", killed)
	}
}
