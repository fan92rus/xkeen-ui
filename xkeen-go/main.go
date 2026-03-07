// Package main is the entry point for XKEEN-GO.
// XKEEN-GO is a lightweight web UI for XKeen on Keenetic routers.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/user/xkeen-go/internal/config"
	"github.com/user/xkeen-go/internal/server"
	"github.com/user/xkeen-go/internal/version"
)

// Build information (set via ldflags)
var (
	appVersion   = "0.1.0"
	buildDate = "unknown"
	gitCommit = "unknown"
)

// Installation paths for Keenetic/Entware
const (
	installBinDir      = "/opt/bin"
	installConfigDir   = "/opt/etc/xkeen-go"
	installConfig      = "/opt/etc/xkeen-go/config.json"
	installInitScript  = "/opt/etc/init.d/xkeen-go"
	installUpdateScript = "/opt/etc/xkeen-go/update.sh"
	installLogFile     = "/opt/var/log/xkeen-go.log"
	installPidFile     = "/var/run/xkeen-go.pid"
	binaryName         = "xkeen-go-keenetic-arm64"
)

// Default config JSON template
const defaultConfigJSON = `{
    "port": 8089,
    "xray_config_dir": "/opt/etc/xray/configs",
    "xkeen_binary": "xkeen",
    "allowed_roots": [
        "/opt/etc/xray",
        "/opt/etc/xkeen",
        "/opt/etc/mihomo",
        "/opt/var/log"
    ],
    "session_secret": "",
    "log_level": "info",
    "cors": {
        "enabled": false,
        "allowed_origins": []
    },
    "auth": {
        "username": "admin",
        "password_hash": "",
        "session_timeout": 24,
        "max_login_attempts": 5,
        "lockout_duration": 5
    }
}`

// Init script template for Keenetic with start-stop-daemon
const initScriptTemplate = `#!/bin/sh
DAEMON=/opt/bin/xkeen-go-keenetic-arm64
CONFIG=/opt/etc/xkeen-go/config.json
PIDFILE=/var/run/xkeen-go.pid
LOGFILE=/opt/var/log/xkeen-go.log
NAME=xkeen-go
DESC="XKEEN-GO Web Interface"

start() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "$NAME is already running"
        return 1
    fi
    echo "Starting $DESC..."
    mkdir -p /opt/var/log
    start-stop-daemon -S -b -m -p "$PIDFILE" -x "$DAEMON" -- -config "$CONFIG" >> "$LOGFILE" 2>&1
    echo "Started $NAME (PID: $(cat $PIDFILE))"
    echo "Logs: $LOGFILE"
}

stop() {
    echo "Stopping $DESC..."
    start-stop-daemon -K -p "$PIDFILE" -x "$DAEMON" 2>/dev/null
    rm -f "$PIDFILE"
    echo "$NAME stopped"
}

status() {
    if [ -f "$PIDFILE" ] && kill -0 $(cat "$PIDFILE") 2>/dev/null; then
        echo "$NAME is running (PID: $(cat $PIDFILE))"
    else
        echo "$NAME is not running"
    fi
}

log() {
    if [ -f "$LOGFILE" ]; then
        tail -f "$LOGFILE"
    else
        echo "Log file not found: $LOGFILE"
    fi
}

case "$1" in
    start)
        start
        ;;
    stop)
        stop
        ;;
    restart)
        stop
        sleep 1
        start
        ;;
    status)
        status
        ;;
    log)
        log
        ;;
    enable)
        echo "$NAME enabled"
        ;;
    disable)
        stop
        echo "$NAME disabled"
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status|log|enable|disable}"
        exit 1
        ;;
esac

exit 0
`

func main() {
	// Parse subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := install(); err != nil {
				log.Fatalf("Installation failed: %v", err)
			}
			os.Exit(0)
		case "uninstall":
			if err := uninstall(); err != nil {
				log.Fatalf("Uninstallation failed: %v", err)
			}
			os.Exit(0)
		case "version", "-version", "--version", "-v":
			printVersion()
			os.Exit(0)
		case "help", "-help", "--help", "-h":
			printUsage()
			os.Exit(0)
		case "-config":
			// -config flag means run server with custom config
			runServer()
			return
		default:
			// Check if it's a flag (starts with -)
			if strings.HasPrefix(os.Args[1], "-") {
				// Unknown flag, show usage
				fmt.Fprintf(os.Stderr, "Unknown flag: %s\n\n", os.Args[1])
				printUsage()
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
			printUsage()
			os.Exit(1)
		}
	}

	// No subcommand - run server
	runServer()
}

func printVersion() {
	fmt.Printf("XKEEN-GO %s\n", appVersion)
	fmt.Printf("Build date: %s\n", buildDate)
	fmt.Printf("Git commit: %s\n", gitCommit)
}

func printUsage() {
	fmt.Println("XKEEN-GO - Web UI for XKeen on Keenetic routers")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s           Start web server\n", binaryName)
	fmt.Printf("  %s install   Install on Keenetic router\n", binaryName)
	fmt.Printf("  %s uninstall Remove from system\n", binaryName)
	fmt.Printf("  %s version   Show version info\n", binaryName)
	fmt.Println()
	fmt.Println("Server options (via config file):")
	fmt.Println("  -config PATH   Path to config file (default: /opt/etc/xkeen-go/config.json)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  ./xkeen-go-keenetic-arm64 install\n")
	fmt.Printf("  /opt/etc/init.d/xkeen-go start\n")
	fmt.Printf("  /opt/etc/init.d/xkeen-go status\n")
}

func runServer() {
	// Default config path (can be overridden with -config flag)
	configPath := installConfig
	for i, arg := range os.Args[1:] {
		if arg == "-config" && i+1 < len(os.Args[1:]) {
			configPath = os.Args[i+2]
		}
	}

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file not found, creating default at %s", configPath)
			cfg = config.DefaultConfig()
			cfg.SessionSecret = generateSecret()
			if saveErr := cfg.SaveConfig(configPath); saveErr != nil {
				log.Fatalf("Failed to create default config: %v", saveErr)
			}
			log.Printf("Default config created. Please edit %s and set a password.", configPath)
		} else {
			log.Fatalf("Failed to load config: %v", err)
		}
	}

	// Initialize version package with ldflags values
	version.SetVersion(appVersion, buildDate, gitCommit)

	// Log startup information
	log.Printf("XKEEN-GO %s starting...", appVersion)
	log.Printf("Config file: %s", configPath)
	log.Printf("Listen port: %d", cfg.Port)
	log.Printf("Xray config dir: %s", cfg.XrayConfigDir)
	log.Printf("Allowed roots: %v", cfg.AllowedRoots)

	// Create server with embedded web files
	srv, err := server.NewServer(cfg, configPath, GetWebFS())
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Handle shutdown in separate goroutine
	go func() {
		sig := <-quit
		log.Printf("Received signal: %v", sig)
		log.Println("Shutting down...")

		// Graceful shutdown with timeout
		done := make(chan struct{})
		go func() {
			if err := srv.Stop(); err != nil {
				log.Printf("Error during shutdown: %v", err)
			}
			close(done)
		}()

		// Wait for shutdown or force exit after 15 seconds
		select {
		case <-done:
			log.Println("Shutdown completed")
		case <-time.After(15 * time.Second):
			log.Println("Shutdown timeout, forcing exit")
		}

		log.Println("Goodbye!")
		os.Exit(0)
	}()

	// Start server (blocks until shutdown)
	log.Printf("Starting HTTP server on :%d", cfg.Port)
	if err := srv.Start(); err != nil {
		log.Printf("Server error: %v", err)
	}
	log.Println("Server exited normally")
}

// install performs the installation on Keenetic router
func install() error {
	fmt.Println("===================================")
	fmt.Println("  XKEEN-GO Installer for Keenetic")
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

	// Disable old init script if exists
	if _, err := os.Stat(installInitScript); err == nil {
		fmt.Println("Disabling old init script...")
		_ = exec.Command(installInitScript, "disable").Run()
	}

	// Remove old binary if exists
	oldBinary := filepath.Join(installBinDir, binaryName)
	if _, err := os.Stat(oldBinary); err == nil {
		fmt.Println("Removing old binary...")
		os.Remove(oldBinary)
	}

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

	// Copy binary to /opt/bin
	targetBin := filepath.Join(installBinDir, binaryName)
	fmt.Printf("Installing binary to %s...\n", targetBin)
	data, err := os.ReadFile(execPath)
	if err != nil {
		return fmt.Errorf("failed to read binary: %w", err)
	}
	if err := os.WriteFile(targetBin, data, 0755); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Create default config if not exists
	if _, err := os.Stat(installConfig); os.IsNotExist(err) {
		fmt.Println("Creating default configuration...")
		// Generate session secret
		secret := generateSecret()
		configContent := strings.Replace(defaultConfigJSON, `"session_secret": ""`, fmt.Sprintf(`"session_secret": "%s"`, secret), 1)
		if err := os.WriteFile(installConfig, []byte(configContent), 0600); err != nil {
			return fmt.Errorf("failed to create config: %w", err)
		}
		fmt.Println()
		fmt.Println("***************************************")
		fmt.Println("*  IMPORTANT: Default credentials     *")
		fmt.Println("*  Username: admin                    *")
		fmt.Println("*  Password: admin                    *")
		fmt.Println("*  PLEASE CHANGE THE PASSWORD ASAP!   *")
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
	if err := os.WriteFile(installInitScript, []byte(initScriptTemplate), 0755); err != nil {
		return fmt.Errorf("failed to create init script: %w", err)
	}

	// Create update script
	fmt.Printf("Creating update script at %s...\n", installUpdateScript)
	if err := os.WriteFile(installUpdateScript, []byte(updateScript), 0755); err != nil {
		return fmt.Errorf("failed to create update script: %w", err)
	}

	// Enable service
	fmt.Println("Enabling service...")
	_ = exec.Command(installInitScript, "enable").Run()

	fmt.Println()
	fmt.Println("===================================")
	fmt.Println("XKEEN-GO installed successfully!")
	fmt.Println("===================================")
	fmt.Println()
	fmt.Println("Files installed:")
	fmt.Printf("  Binary:     %s\n", targetBin)
	fmt.Printf("  Config:     %s\n", installConfig)
	fmt.Printf("  Init script: %s\n", installInitScript)
	fmt.Printf("  Log file:   %s\n", installLogFile)
	fmt.Println()
	fmt.Println("To start the service:")
	fmt.Printf("  %s start    # Background mode\n", installInitScript)
	fmt.Printf("  %s    # Foreground (see logs in console)\n", targetBin)
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Printf("  Start:   %s start\n", installInitScript)
	fmt.Printf("  Stop:    %s stop\n", installInitScript)
	fmt.Printf("  Restart: %s restart\n", installInitScript)
	fmt.Printf("  Status:  %s status\n", installInitScript)
	fmt.Printf("  Logs:    %s log\n", installInitScript)
	fmt.Println()
	fmt.Printf("Web interface: http://<router-ip>:8089\n")
	fmt.Println()

	return nil
}

// uninstall removes xkeen-go from the system
func uninstall() error {
	fmt.Println("XKEEN-GO Uninstallation Script")
	fmt.Println("==============================")
	fmt.Println()

	// Check if running as root
	if os.Getuid() != 0 {
		return fmt.Errorf("this command must be run as root (use sudo or run as root)")
	}

	// Stop service via init script
	fmt.Println("Stopping xkeen-go service...")
	if _, err := os.Stat(installInitScript); err == nil {
		_ = exec.Command(installInitScript, "stop").Run()
		_ = exec.Command(installInitScript, "disable").Run()
	}

	// Kill any remaining processes
	stopProcess()

	// Remove PID file
	os.Remove(installPidFile)

	// Remove init script
	if _, err := os.Stat(installInitScript); err == nil {
		fmt.Println("Removing init script...")
		os.Remove(installInitScript)
	}

	// Remove update script
	if _, err := os.Stat(installUpdateScript); err == nil {
		fmt.Println("Removing update script...")
		os.Remove(installUpdateScript)
	}

	// Remove binary
	binaryPath := filepath.Join(installBinDir, binaryName)
	if _, err := os.Stat(binaryPath); err == nil {
		fmt.Println("Removing binary...")
		os.Remove(binaryPath)
	}

	// Ask about config directory
	if _, err := os.Stat(installConfigDir); err == nil {
		fmt.Println()
		fmt.Printf("Remove config directory %s? [y/N]: ", installConfigDir)
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(answer) == "y" || strings.ToLower(answer) == "yes" {
			fmt.Println("Removing config directory...")
			os.RemoveAll(installConfigDir)
		} else {
			fmt.Println("Keeping config directory")
		}
	}

	fmt.Println()
	fmt.Println("Uninstallation complete!")
	fmt.Println("XKEEN-GO has been removed from your system.")

	return nil
}

// stopProcess stops any running xkeen-go processes (except current)
func stopProcess() {
	myPID := os.Getpid()

	// Try to find and kill by pgrep (exclude current process)
	cmd := exec.Command("pgrep", "-f", binaryName)
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		pids := strings.Fields(strings.TrimSpace(string(output)))
		for _, pidStr := range pids {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			// Skip current process
			if pid == myPID {
				continue
			}
			fmt.Printf("Killing process by PID: %d\n", pid)
			_ = exec.Command("kill", pidStr).Run()
		}
	}

	// Wait a moment
	exec.Command("sleep", "1").Run()

	// Force kill if still running
	cmd = exec.Command("pgrep", "-f", binaryName)
	output, err = cmd.Output()
	if err == nil && len(output) > 0 {
		pids := strings.Fields(strings.TrimSpace(string(output)))
		for _, pidStr := range pids {
			var pid int
			fmt.Sscanf(pidStr, "%d", &pid)
			// Skip current process
			if pid == myPID {
				continue
			}
			fmt.Println("Force killing xkeen-go...")
			_ = exec.Command("kill", "-9", pidStr).Run()
		}
	}
}

// generateSecret generates a random secret for session encryption
func generateSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to less secure but functional secret
		return "xkeen-go-change-this-secret-" + buildDate
	}
	return base64.StdEncoding.EncodeToString(b)
}
