package handlers

// Interface status helpers: parsing `awg show` output and name validation.

import (
	"errors"
	"log"
	"strings"

	"os/exec"
)

// ---------- Helpers ----------

// getActiveInterfaces returns a map of active AWG interface names to their addresses.
func (h *AWGHandler) getActiveInterfaces() map[string]string {
	result := make(map[string]string)

	output, err := awgShowFn()
	if err != nil {
		// awg not installed or not available — log once and return empty
		if errors.Is(err, exec.ErrNotFound) {
			log.Println("[awg] awg binary not found, skipping interface detection")
		} else {
			log.Printf("[awg] awg show failed: %v", err)
		}
		return result
	}

	// Parse `awg show` output. Format (example):
	//   interface: warp
	//     public key: ...
	//     private key: ...
	//     listening port: 51820
	//   peer: ...
	//     endpoint: ...
	//     allowed ips: ...
	//   ...
	lines := strings.Split(string(output), "\n")
	var currentIface string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			currentIface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
			result[currentIface] = ""
		} else if strings.HasPrefix(line, "address:") && currentIface != "" {
			result[currentIface] = strings.TrimSpace(strings.TrimPrefix(line, "address:"))
		}
	}

	return result
}

// validateAWGName checks that a config name contains only safe characters.
func validateAWGName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") ||
		strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
