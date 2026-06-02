//go:build linux || darwin || freebsd

package handlers

import (
	"os"
	"syscall"
)

// getFileInode returns the inode number of a file, or 0 if unavailable.
func getFileInode(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}
