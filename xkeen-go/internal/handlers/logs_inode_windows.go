package handlers

import "os"

// getFileInode returns 0 on Windows (no inode support).
func getFileInode(_ os.FileInfo) uint64 {
	return 0
}
