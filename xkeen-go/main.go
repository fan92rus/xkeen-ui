// Package main is the entry point for XKEEN-UI.
// XKEEN-UI is a lightweight web UI for XKeen on Keenetic routers.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/handlers"
	"github.com/fan92rus/xkeen-ui/internal/server"
	"github.com/fan92rus/xkeen-ui/internal/utils"
	"github.com/fan92rus/xkeen-ui/internal/version"
)

// Build information (set via ldflags)
var (
	appVersion   = "0.1.0"
	buildDate = "unknown"
	gitCommit = "unknown"
)

// Installation paths for Keenetic/Entware
const (
	installBinDir       = "/opt/bin"
	installConfigDir    = "/opt/etc/xkeen-ui"
	installConfig       = "/opt/etc/xkeen-ui/config.json"
	installInitScript   = "/opt/etc/init.d/xkeen-ui"
	installSymlink      = "/opt/bin/xkeen-ui"
	installUpdateScript = "/opt/etc/xkeen-ui/update.sh"
	installLogFile      = "/opt/var/log/xkeen-ui.log"
	installPidFile      = "/var/run/xkeen-ui.pid"
	// S70 runs before S89* scripts that may fail (awg-server, warp-routing)
	// and abort the Entware rc.unslung boot sequence. S99 was too late.
	installAutoStart    = "/opt/etc/init.d/S70xkeen-ui"
	installOldAutoStart = "/opt/etc/init.d/S99xkeen-ui"
)

// binaryName is the architecture-specific binary name
var binaryName = utils.GetBinaryNameForArch()

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
        "password_hash": "",
        "session_timeout": 24,
        "max_login_attempts": 5,
        "lockout_duration": 5
    }
}`

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
		case "check":
			checkProcess()
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
	fmt.Printf("XKEEN-UI %s\n", appVersion)
	fmt.Printf("Build date: %s\n", buildDate)
	fmt.Printf("Git commit: %s\n", gitCommit)
}

func checkProcess() {
	pidData, err := os.ReadFile(installPidFile)
	if err != nil {
		os.Exit(1)
	}
	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		os.Exit(1)
	}
	proc, err := os.FindProcess(pid)
	if err != nil || proc == nil {
		os.Exit(1)
	}
	// Sends signal 0 to check if process exists (no side effects)
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func printUsage() {
	fmt.Println("XKEEN-UI - Web UI for XKeen on Keenetic routers")
	fmt.Println()
	fmt.Println("USAGE:")
	fmt.Printf("    %s [command] [options]\n", binaryName)
	fmt.Println()
	fmt.Println("COMMANDS:")
	fmt.Println("    install     Install on Keenetic router (requires root)")
	fmt.Println("    uninstall   Remove from system (requires root)")
	fmt.Println("    version     Show version information")
	fmt.Println("    help        Show this help message")
	fmt.Println()
	fmt.Println("OPTIONS:")
	fmt.Println("    -config <path>    Path to config file")
	fmt.Println("                      (default: /opt/etc/xkeen-ui/config.json)")
	fmt.Println()
	fmt.Println("SERVICE CONTROL (after install):")
	fmt.Println("    xkeen-ui start      Start the service")
	fmt.Println("    xkeen-ui stop       Stop the service")
	fmt.Println("    xkeen-ui restart    Restart the service")
	fmt.Println("    xkeen-ui status     Check service status")
	fmt.Println("    xkeen-ui uninstall  Remove from system")
	fmt.Println("    xkeen-ui log        Tail the log file")
	fmt.Println()
	fmt.Println("EXAMPLES:")
	fmt.Printf("    ./%s install\n", binaryName)
	fmt.Println("    xkeen-ui start")
	fmt.Println("    xkeen-ui -config /path/to/config.json")
	fmt.Println()
	fmt.Println("FILES:")
	fmt.Println("    /opt/bin/xkeen-ui-keenetic-arm64    Binary")
	fmt.Println("    /opt/etc/xkeen-ui/config.json       Configuration")
	fmt.Println("    /opt/etc/init.d/xkeen-ui            Init script")
	fmt.Println("    /opt/var/log/xkeen-ui.log           Log file")
	fmt.Println()
	fmt.Printf("Web interface: http://<router-ip>:8089\n")
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
	log.Printf("XKEEN-UI %s starting...", appVersion)
	log.Printf("Config file: %s", configPath)
	log.Printf("Listen port: %d", cfg.Port)
	log.Printf("Xray config dir: %s", cfg.XrayConfigDir)
	log.Printf("Allowed roots: %v", cfg.AllowedRoots)

	// Run startup migrations: idempotent self-healing tasks that run on
	// every boot (e.g. recreate deleted config files). Not tracked.
	runStartupMigrations()

	// Create server with embedded web files
	srv, err := server.NewServer(cfg, configPath, GetWebFS())
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Handle shutdown in separate goroutine
	// Also listen on update shutdown channel so update handler can trigger graceful restart
	go func() {
		select {
		case sig := <-quit:
			log.Printf("Received signal: %v", sig)
		case <-handlers.UpdateShutdownCh:
			log.Println("Shutdown requested by update handler")
		}
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

