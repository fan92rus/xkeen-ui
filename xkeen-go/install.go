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
		if err := os.MkdirAll(dir, 0o750); err != nil {
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
			_ = os.Remove(targetBin)
		}

		// Copy binary to /opt/bin
		fmt.Printf("Installing binary to %s...\n", targetBin)
		data, err := os.ReadFile(execPath)
		if err != nil {
			return fmt.Errorf("failed to read binary: %w", err)
		}
		//nolint:gosec // binary needs execute permission
		if err := os.WriteFile(targetBin, data, 0o755); err != nil {
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
		configContent := strings.Replace(defaultConfigJSON, `"session_secret": ""`, fmt.Sprintf(`"session_secret": %q`, secret), 1)
		configContent = strings.Replace(configContent, `"password_hash": ""`, fmt.Sprintf(`"password_hash": %q`, string(passwordHash)), 1)

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

		if err := os.WriteFile(installConfig, configBytes, 0o600); err != nil {
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
	if err := os.MkdirAll(initDir, 0o750); err != nil {
		return fmt.Errorf("failed to create init directory: %w", err)
	}
	//nolint:gosec // init script needs execute
	if err := os.WriteFile(installInitScript, []byte(getInitScript(binaryName)), 0o755); err != nil {
		return fmt.Errorf("failed to create init script: %w", err)
	}

	// Create symlink for easy access
	fmt.Printf("Creating symlink at %s...\n", installSymlink)
	_ = os.Remove(installSymlink) // Remove existing symlink if any
	if err := os.Symlink(installInitScript, installSymlink); err != nil {
		fmt.Printf("Warning: failed to create symlink: %v\n", err)
	}

	// Create update script
	fmt.Printf("Creating update script at %s...\n", installUpdateScript)
	//nolint:gosec // update script needs execute
	if err := os.WriteFile(installUpdateScript, []byte(updateScript), 0o755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	// Enable autostart — Entware runs /opt/etc/init.d/S* at boot.
	// S70 (not S99) so we start before S89* scripts (awg-server, warp-routing)
	// that may return non-zero and abort the rc.unslung boot sequence.
	fmt.Println("Enabling autostart...")
	// Remove legacy S99 symlink if upgrading from an older xkeen-ui version
	_ = os.Remove(installOldAutoStart)
	_ = os.Remove(installAutoStart)
	if err := os.Symlink(installInitScript, installAutoStart); err != nil {
		fmt.Printf("Warning: failed to create autostart symlink: %v\n", err)
	}
	// Also create in rc.d/ for Entware variants that use it.
	// Clean both S70 and legacy S99 links before creating new ones.
	rcDir := "/opt/etc/init.d/rc.d"
	_ = os.MkdirAll(rcDir, 0o750)
	for _, suffix := range []string{"S70xkeen-ui", "S99xkeen-ui"} {
		_ = os.Remove(filepath.Join(rcDir, suffix))
	}
	_ = os.Remove(filepath.Join(rcDir, "K01xkeen-ui"))
	_ = os.Symlink(installInitScript, filepath.Join(rcDir, "S70xkeen-ui"))
	_ = os.Symlink(installInitScript, filepath.Join(rcDir, "K01xkeen-ui"))

	// Clean up stale init scripts from older xkeen-ui versions that can
	// break boot on some Keenetic setups (non-zero exit aborts rc.unslung).
	// Create cron watchdog for auto-restart on crash.
	//
	// This is a BACKUP autostart mechanism.  The PRIMARY mechanism is the
	// NDM netfilter hook (below), which fires at boot when NDM initializes
	// the firewall.  The cron watchdog covers mid-run crashes and fires
	// every minute if crond is alive.
	//
	// Keenetic's busybox crond does NOT scan /opt/etc/cron.d/ — it only reads
	// user crontabs ("crontab -l/-e").  Many installations were bitten by this:
	// the watchdog file was written but never executed, so xkeen-ui stayed dead
	// after a reboot or crash.  We now install the watchdog into the root user's
	// crontab directly.
	if err := installCronWatchdog(installAutoStart, installLogFile); err != nil {
		fmt.Printf("Warning: failed to install cron watchdog: %v\n", err)
	} else {
		fmt.Println("Cron watchdog installed (backup: checks every minute, restart if down)")
	}

	// Create NDM netfilter hook — the PRIMARY autostart mechanism on Keenetic.
	//
	// On Keenetic routers, NDM (the core system process) does NOT call
	// /opt/etc/init.d/rc.unslung at boot.  Instead, it triggers scripts in
	// /opt/etc/ndm/netfilter.d/ when the firewall is initialized.  This is
	// how xkeen itself starts xray (via proxy.sh in the same directory).
	//
	// Unlike crond (which may be killed by OOM or start late), NDM is the
	// init's direct child and runs reliably at boot.  The hook calls
	// 'xkeen-ui check || xkeen-ui start', so it revives the service on every
	// firewall event if it's down, and is a near-instant no-op if it's up.
	if err := installNDMHook(installAutoStart, installLogFile); err != nil {
		fmt.Printf("Warning: failed to install NDM hook: %v\n", err)
	} else {
		fmt.Println("NDM netfilter hook installed (primary boot autostart)")
	}

	// Run one-time migrations (like DB migrations: each runs exactly once).
	fmt.Println()
	fmt.Println("Running migrations...")
	runMigrations()

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
	//nolint:gosec // uninstall script needs execute
	if err := os.WriteFile(tmpScript, []byte(uninstallScript), 0o755); err != nil {
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
		_, _ = fmt.Sscanf(pidStr, "%d", &pid)
		if pid == myPID {
			continue
		}
		fmt.Printf("Killing process by PID: %d\n", pid)
		_ = exec.Command("kill", pidStr).Run()
	}

	// Wait a moment
	_ = exec.Command("sleep", "1").Run()

	// Force kill if still running
	pids = findProcessPIDs()
	for _, pidStr := range pids {
		var pid int
		_, _ = fmt.Sscanf(pidStr, "%d", &pid)
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

// cronWatchdogMarker is the comment that identifies our watchdog line in the
// root crontab, so we can find and replace it on upgrade without duplicating.
const cronWatchdogMarker = "# xkeen-ui-watchdog (auto-managed)"

// installCronWatchdog installs a per-minute watchdog into the root user's
// crontab (the only place Keenetic busybox crond actually scans).  The
// watchdog runs "xkeen-ui check || xkeen-ui start", so a crashed or
// reboot-killed service is revived within 60 seconds.
//
// We also remove any stale /opt/etc/cron.d/xkeen-ui-watchdog left by older
// builds — busybox crond never read it, but it is misleading to leave behind.
func installCronWatchdog(initScript, logFile string) error {
	// 1. Remove legacy cron.d file (busybox crond does not read it).
	_ = os.Remove("/opt/etc/cron.d/xkeen-ui-watchdog")

	// 2. Read current root crontab.
	var current []byte
	if out, err := exec.Command("crontab", "-l").Output(); err == nil {
		current = out
	}

	markerLine := cronWatchdogMarker
	watchdogLine := fmt.Sprintf(
		"* * * * * %s check || %s start >> %s 2>&1",
		initScript, initScript, logFile,
	)
	// @reboot ensures xkeen-ui starts as soon as crond is up at boot,
	// without waiting for the next minute boundary.  On some Keenetic
	// setups rc.unslung never runs (NDM does not call /etc/init.d/rcS),
	// so the S70 symlink is never processed and crond is the only
	// reliable startup path.
	rebootLine := fmt.Sprintf(
		"@reboot sleep 10 && %s start >> %s 2>&1",
		initScript, logFile,
	)

	// 3. Rebuild crontab: keep all lines except our previous watchdog entries.
	//
	// CRITICAL: the filter must be narrow.  Other crontab entries (log
	// rotation, monitoring) legitimately reference "xkeen-ui" in file
	// paths like /opt/var/log/xkeen-ui.log.  We only want to strip our
	// own watchdog lines, which always contain the init.d path + "check".
	var kept []string
	for _, line := range strings.Split(string(current), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == markerLine {
			continue // drop our marker comment
		}
		// Match watchdog commands: references init.d + xkeen-ui + check.
		// This preserves log rotation and unrelated entries that merely
		// mention xkeen-ui.log without referencing the init script.
		if strings.Contains(line, "init.d") &&
			strings.Contains(line, "xkeen-ui") &&
			strings.Contains(line, "check") {
			continue // drop old watchdog command
		}
		// Drop old @reboot line (references init.d + xkeen-ui + start).
		if strings.HasPrefix(trimmed, "@reboot") &&
			strings.Contains(line, "init.d") &&
			strings.Contains(line, "xkeen-ui") {
			continue
		}
		if trimmed != "" {
			kept = append(kept, line)
		}
	}
	newCrontab := strings.Join(kept, "\n")
	if newCrontab != "" {
		newCrontab += "\n"
	}
	newCrontab += markerLine + "\n" + watchdogLine + "\n" + rebootLine + "\n"

	// 4. Install via "crontab -".
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("crontab update failed: %w", err)
	}
	return nil
}

// installNDMHook creates a script in /opt/etc/ndm/netfilter.d/ that starts
// xkeen-ui when NDM initializes the firewall.  This is the PRIMARY autostart
// mechanism on Keenetic routers, where NDM does not call rc.unslung at boot.
//
// NDM fires netfilter.d scripts at boot (during firewall init) and on every
// firewall change.  The hook runs 'check || start', so it is a near-instant
// no-op when xkeen-ui is already running and revives it when down.
func installNDMHook(initScript, logFile string) error {
	hookDir := filepath.Dir(installNDMHookPath)
	//nolint:gosec // NDM hook dir needs 0755 — other hooks (proxy.sh, nfqws2) use the same
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return fmt.Errorf("failed to create NDM hook directory %s: %w", hookDir, err)
	}

	// Remove any old hook from a previous install, then write the new one.
	_ = os.Remove(installNDMHookPath)

	hookContent := fmt.Sprintf(`#!/bin/sh
# xkeen-ui autostart via NDM netfilter hook (auto-generated).
# NDM fires netfilter.d scripts at boot (firewall init) and on firewall
# changes.  The check is near-instant (PID file + signal 0); start only
# fires when xkeen-ui is down, making this safe to call on every event.
%s check 2>/dev/null || %s start >>%s 2>&1 &
`, initScript, initScript, logFile)

	//nolint:gosec // NDM hook script needs execute permission
	if err := os.WriteFile(installNDMHookPath, []byte(hookContent), 0o755); err != nil {
		return fmt.Errorf("failed to write NDM hook %s: %w", installNDMHookPath, err)
	}
	return nil
}
