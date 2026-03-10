package utils

import (
	"log"
	"runtime"
)

// GetBinaryNameForArch returns the appropriate binary name for the current architecture.
func GetBinaryNameForArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "xkeen-ui-keenetic-arm64"
	case "mips", "mipsle":
		return "xkeen-ui-keenetic-mips"
	case "amd64":
		// AMD64 for development/testing - use arm64 as fallback
		return "xkeen-ui-keenetic-arm64"
	default:
		log.Printf("WARNING: Unknown architecture %s, defaulting to arm64", runtime.GOARCH)
		return "xkeen-ui-keenetic-arm64"
	}
}
