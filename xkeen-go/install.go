// Package main provides the entry point for XKEEN-UI.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// install copies the binary to /opt/bin, creates config, init script, symlinks,
// and starts the service.
func install() error {
	fmt.Println("===================================")
	fmt.Println("  XKEEN-UI Installer for Keenetic")
	fmt.Println("===================================")
	fmt.Println()

	// Check if running as root
	if os.Getuid() != 0 {
		return fmt.Errorf("this command must be run as root (use sudo or run as root)")
	}

	// Get executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Stop existing service if running
	fmt.Println("Checking for existing installation...")
	stopProcess()

	// Create directories
	fmt.Println("Creating directories...")
	dirs := []string{
		installBinDir,
		installConfigDir,
		filepath.Dir(installLogFile),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Check if binary is already in the target location
	targetBin := filepath.Join(installBinDir, binaryName)
	needsCopy := execPath != targetBin

	if needsCopy {
		// Remove old binary if exists (only if running from different location)
		if _, err := os.Stat(targetBin); err == nil {
			fmt.Println("Removing old binary...")
			os.Remove(targetBin)
		}

		// Copy binary to /opt/bin
		fmt.Printf("Installing binary to %s...\n", targetBin)
		data, err := os.ReadFile(execPath)
		if err != nil {
			return fmt.Errorf("failed to read binary: %w", err)
		}
		if err := os.WriteFile(targetBin, data, 0755); err != nil {
			return fmt.Errorf("failed to copy binary: %w", err)
		}
	} else {
		fmt.Println("Binary already in target location, skipping copy")
	}

	// Create default config if not exists
	if _, err := os.Stat(installConfig); os.IsNotExist(err) {
		fmt.Println("Creating default configuration...")
		// Generate session secret
		secret := generateSecret()

		// Generate bcrypt hash for default password "admin"
		passwordHash, err := bcrypt.GenerateFromPassword([]byte("admin"), 12)
		if err != nil {
			return fmt.Errorf("failed to hash default password: %w", err)
		}

		// Create config with default credentials and force password change
		configContent := strings.Replace(defaultConfigJSON, `"session_secret": ""`, fmt.Sprintf(`"session_secret": "%s"`, secret), 1)
		configContent = strings.Replace(configContent, `"password_hash": ""`, fmt.Sprintf(`"password_hash": "%s"`, string(passwordHash)), 1)

		// Add force_password_change flag for default credentials
		var configMap map[string]interface{}
		if err := json.Unmarshal([]byte(configContent), &configMap); err != nil {
			return fmt.Errorf("failed to parse default config: %w", err)
		}
		if auth, ok := configMap["auth"].(map[string]interface{}); ok {
			auth["force_password_change"] = true
		}
		configBytes, err := json.MarshalIndent(configMap, "", "    ")
		if err != nil {
			return fmt.Errorf("failed to marshal config: %w", err)
		}

		if err := os.WriteFile(installConfig, configBytes, 0600); err != nil {
			return fmt.Errorf("failed to create config: %w", err)
		}
		fmt.Println()
		fmt.Println("***************************************")
		fmt.Println("*  IMPORTANT: Default password        *")
		fmt.Println("*  Password: admin                    *")
		fmt.Println("*  YOU MUST CHANGE IT ON LOGIN!       *")
		fmt.Println("***************************************")
	} else {
		fmt.Println("Configuration file already exists, keeping it")
	}

	// Create init script
	fmt.Printf("Creating init script at %s...\n", installInitScript)
	initDir := filepath.Dir(installInitScript)
	if err := os.MkdirAll(initDir, 0755); err != nil {
		return fmt.Errorf("failed to create init directory: %w", err)
	}
	if err := os.WriteFile(installInitScript, []byte(getInitScript(binaryName)), 0755); err != nil {
		return fmt.Errorf("failed to create init script: %w", err)
	}

	// Create symlink for easy access
	fmt.Printf("Creating symlink at %s...\n", installSymlink)
	os.Remove(installSymlink) // Remove existing symlink if any
	if err := os.Symlink(installInitScript, installSymlink); err != nil {
		fmt.Printf("Warning: failed to create symlink: %v\n", err)
	}

	// Create update script
	fmt.Printf("Creating update script at %s...\n", installUpdateScript)
	if err := os.WriteFile(installUpdateScript, []byte(updateScript), 0755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	// Enable autostart — Entware runs /opt/etc/init.d/S* at boot.
	// S70 (not S99) so we start before S89* scripts (awg-server, warp-routing)
	// that may return non-zero and abort the rc.unslung boot sequence.
	fmt.Println("Enabling autostart...")
	// Remove legacy S99 symlink if upgrading from an older xkeen-ui version
	os.Remove(installOldAutoStart)
	os.Remove(installAutoStart)
	if err := os.Symlink(installInitScript, installAutoStart); err != nil {
		fmt.Printf("Warning: failed to create autostart symlink: %v\n", err)
	}
	// Also create in rc.d/ for Entware variants that use it.
	// Clean both S70 and legacy S99 links before creating new ones.
	rcDir := "/opt/etc/init.d/rc.d"
	os.MkdirAll(rcDir, 0755)
	for _, suffix := range []string{"S70xkeen-ui", "S99xkeen-ui"} {
		os.Remove(filepath.Join(rcDir, suffix))
	}
	os.Remove(filepath.Join(rcDir, "K01xkeen-ui"))
	os.Symlink(installInitScript, filepath.Join(rcDir, "S70xkeen-ui"))
	os.Symlink(installInitScript, filepath.Join(rcDir, "K01xkeen-ui"))

	// Clean up stale init scripts from older xkeen-ui versions that can
	// break boot on some Keenetic setups (non-zero exit aborts rc.unslung).
	cleanupStaleInitScripts()

	// Create cron watchdog for auto-restart on crash
	cronDir := "/opt/etc/cron.d"
	cronFile := filepath.Join(cronDir, "xkeen-ui-watchdog")
	cronContent := fmt.Sprintf(
		"* * * * * root %s check || %s start >> %s 2>&1\n",
		installSymlink, installSymlink, installLogFile,
	)
	if err := os.MkdirAll(cronDir, 0755); err != nil {
		fmt.Printf("Warning: failed to create cron directory: %v\n", err)
	} else if err := os.WriteFile(cronFile, []byte(cronContent), 0644); err != nil {
		fmt.Printf("Warning: failed to create cron watchdog: %v\n", err)
	} else {
		fmt.Println("Cron watchdog created (checks every minute, restart if down)")
	}

	// Restart cron to pick up new watchdog
	exec.Command("killall", "-HUP", "crond").Run()

	fmt.Println()
	fmt.Println("===================================")
	fmt.Println("XKEEN-UI installed successfully!")
	fmt.Println("===================================")
	fmt.Println()
	fmt.Println("Files installed:")
	fmt.Printf("  Binary:      %s\n", targetBin)
	fmt.Printf("  Config:      %s\n", installConfig)
	fmt.Printf("  Init script: %s\n", installInitScript)
	fmt.Printf("  Symlink:     %s -> init script\n", installSymlink)
	fmt.Printf("  Log file:    %s\n", installLogFile)
	fmt.Println()

	// Auto-start service after installation
	fmt.Println("Starting service...")
	startCmd := exec.Command("sh", "-c", "(sh "+installInitScript+" start </dev/null >>"+installLogFile+" 2>&1 &)")
	if err := startCmd.Run(); err != nil {
		fmt.Printf("Warning: failed to start service: %v\n", err)
		fmt.Println("Please start manually: xkeen-ui start")
	} else {
		fmt.Println("Service started successfully")
	}
	fmt.Println()

	fmt.Println("Commands:")
	fmt.Println("  Start:   xkeen-ui start")
	fmt.Println("  Stop:    xkeen-ui stop")
	fmt.Println("  Restart: xkeen-ui restart")
	fmt.Println("  Status:  xkeen-ui status")
	fmt.Println("  Check:   xkeen-ui check (exit 0=running, 1=stopped — for cron)")
	fmt.Println("  Logs:    xkeen-ui log")
	fmt.Println()
	fmt.Printf("Web interface: http://<router-ip>:8089\n")
	fmt.Println()

	return nil
}

// uninstall removes xkeen-ui from the system by launching an external script.
func uninstall() error {
	// Check if running as root
	if os.Getuid() != 0 {
		return fmt.Errorf("this command must be run as root (use sudo or run as root)")
	}

	// Write uninstall script to temp location
	tmpScript := "/tmp/xkeen-ui-uninstall.sh"
	if err := os.WriteFile(tmpScript, []byte(uninstallScript), 0755); err != nil {
		return fmt.Errorf("failed to create uninstall script: %w", err)
	}

	// Launch script in background via shell and exit immediately
	cmd := exec.Command("sh", "-c", "sh "+tmpScript+" </dev/null >>"+installLogFile+" 2>&1 &")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start uninstall script: %w", err)
	}

	fmt.Println("Uninstall script started. XKEEN-UI will be removed shortly.")
	return nil
}

// stopProcess stops any running xkeen-ui processes (except current).
func stopProcess() {
	myPID := os.Getpid()

	// Use ps+grep+awk instead of pgrep for BusyBox compatibility.
	// pgrep may not be available on all Keenetic builds.
	pids := findProcessPIDs()
	for _, pidStr := range pids {
		var pid int
		fmt.Sscanf(pidStr, "%d", &pid)
		if pid == myPID {
			continue
		}
		fmt.Printf("Killing process by PID: %d\n", pid)
		_ = exec.Command("kill", pidStr).Run()
	}

	// Wait a moment
	exec.Command("sleep", "1").Run()

	// Force kill if still running
	pids = findProcessPIDs()
	for _, pidStr := range pids {
		var pid int
		fmt.Sscanf(pidStr, "%d", &pid)
		if pid == myPID {
			continue
		}
		fmt.Println("Force killing xkeen-ui...")
		_ = exec.Command("kill", "-9", pidStr).Run()
	}
}

// findProcessPIDs returns PIDs of all xkeen-ui processes via ps+grep+awk.
// Compatible with BusyBox where pgrep may be unavailable.
func findProcessPIDs() []string {
	cmd := exec.Command("sh", "-c", "ps 2>/dev/null | grep 'xkeen-ui' | grep -v grep | grep -v uninstall | awk '{print $1}'")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil
	}
	return strings.Fields(strings.TrimSpace(string(output)))
}

// generateSecret generates a random secret for session encryption.
func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to less secure but functional secret
		return "xkeen-ui-change-this-secret-" + buildDate
	}
	return base64.StdEncoding.EncodeToString(b)
}

// cleanupStaleInitScripts removes legacy init scripts that can break the
// Entware boot sequence. On some Keenetic setups, rc.unslung sources each
// S* script and aborts the entire sequence when one returns non-zero.
// We disable known-offending scripts that belong to superseded setups.
func cleanupStaleInitScripts() {
	// S89amnezia-wg-quick: old AmneziaWG client script that references
	// /opt/etc/amnezia/amneziawg/awg0.conf — typically missing on systems
	// that have migrated to xkeen-managed AWG (server.conf). Its failure
	// (awg-quick: ... does not exist) aborts rc.unslung before S99 runs.
	stale := []string{
		"/opt/etc/init.d/S89amnezia-wg-quick",
	}
	for _, s := range stale {
		if _, err := os.Stat(s); err == nil {
			disabled := s + ".disabled"
			// Don't clobber an existing .disabled backup
			if _, err := os.Stat(disabled); err != nil {
				os.Rename(s, disabled)
				fmt.Printf("Disabled stale init script: %s (was breaking boot)\n", s)
			} else {
				os.Remove(s)
				fmt.Printf("Removed stale init script: %s\n", s)
			}
		}
	}
}
