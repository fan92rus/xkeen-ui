// Package server provides HTTP server functionality for XKEEN-UI.
package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/fan92rus/xkeen-ui/internal/config"
	"github.com/fan92rus/xkeen-ui/internal/handlers"
	"github.com/fan92rus/xkeen-ui/internal/subscription"
	"github.com/fan92rus/xkeen-ui/internal/utils"
)

// Server represents the HTTP server.
type Server struct {
	cfg        *config.Config
	configPath string
	http       *http.Server
	router     *mux.Router
	middleware *Middleware
	sessions   *sessionStore
	security   *securityService
	webFS      fs.FS

	// Real handlers
	configHandler       *handlers.ConfigHandler
	serviceHandler      *handlers.ServiceHandler
	logsHandler         *handlers.LogsHandler
	settingsHandler     *handlers.SettingsHandler
	commandsHandler     *handlers.CommandsHandler
	updateHandler       *handlers.UpdateHandler
	interactiveHandler  *handlers.InteractiveHandler
	subscriptionHandler *handlers.SubscriptionHandler
	metricsHandler      *handlers.MetricsHandler
	// metricsPort mirrors cfg.MetricsPort for lock-free reads from the
	// poller goroutine. 0 = disabled. Updated by the OnMetricsChange
	// callback; the handler's urlFn reads it on every poll.
	metricsPort        atomic.Int64
	commandRegistry    *handlers.CommandRegistry
	installHandler     *handlers.InstallHandler
	awgHandler                *handlers.AWGHandler
	diagnosticsHandler        *handlers.DiagnosticsHandler
	routingCategoriesHandler  *handlers.RoutingCategoriesHandler

	// Shutdown state
	shutdown    bool
	mu          sync.RWMutex
	defaultHash string // cached bcrypt hash for default password
}

// NewServer creates a new HTTP server.
// Returns an error if the server cannot be initialized with a valid backup directory.
func NewServer(cfg *config.Config, configPath string, webFS fs.FS) (*Server, error) {
	// Initialize services
	sessionTimeout := time.Duration(cfg.Auth.SessionTimeout) * time.Hour
	sessions := newSessionStore(sessionTimeout)
	security := newSecurityService()

	// Initialize middleware
	middleware := NewMiddleware(sessions, security)
	middleware.SetTrustProxyHeaders(cfg.TrustProxyHeaders)
	middleware.SetCookieSecure(cfg.CookieSecure)

	// Create router
	router := mux.NewRouter()

	s := &Server{
		cfg:        cfg,
		configPath: configPath,
		router:     router,
		middleware: middleware,
		sessions:   sessions,
		security:   security,
		webFS:      webFS,
	}

	// Validate and determine backup directory
	backupDir, err := validateAndResolveBackupPath(cfg.XrayConfigDir, cfg.AllowedRoots)
	if err != nil {
		return nil, fmt.Errorf("failed to validate backup path: %w", err)
	}

	// Initialize handlers from handlers package
	s.configHandler = handlers.NewConfigHandler(cfg.AllowedRoots, backupDir, cfg.XrayConfigDir, cfg.MihomoConfigDir, cfg.AWGConfigDir, configPath, cfg.Mode)
	s.serviceHandler = handlers.NewServiceHandler()
	s.settingsHandler = handlers.NewSettingsHandler(cfg.AllowedRoots, cfg.XrayConfigDir, backupDir, cfg, configPath,
		func(port int) {
			// Option E: enabling/disabling metrics is a data update, not a
			// lifecycle operation. The handler stays alive for the whole
			// process; we just mirror the new port so the poller picks it up
			// on the next tick. No handler recreation, no Close, no race.
			s.metricsPort.Store(int64(port))
		},
	)

	// Always create the metrics handler (one instance for the whole process
	// lifetime). Its urlFn resolves the target from the live metricsPort
	// atomic: empty string when disabled (port 0) → reports unavailable
	// without polling Xray; non-empty when enabled → polls Xray.
	s.metricsPort.Store(int64(cfg.MetricsPort))
	s.metricsHandler = handlers.NewMetricsHandlerDynamic(
		func() string {
			p := s.metricsPort.Load()
			if p == 0 {
				return ""
			}
			return fmt.Sprintf("http://127.0.0.1:%d", p)
		},
		5*time.Second,
		cfg.CORS.AllowedOrigins,
	)

	s.logsHandler = handlers.NewLogsHandler(handlers.LogsConfig{
		AllowedRoots:   cfg.AllowedRoots,
		AllowedOrigins: cfg.CORS.AllowedOrigins,
		LogFiles: []string{
			"/opt/var/log/xray/access.log",
			"/opt/var/log/xray/error.log",
		},
	})
	s.commandRegistry = handlers.NewCommandRegistry(handlers.DefaultXKeenPath)
	if cfg.XkeenBinary != "" {
		s.commandRegistry = handlers.NewCommandRegistry(cfg.XkeenBinary)
	}
	s.commandsHandler = handlers.NewCommandsHandler(s.commandRegistry)
	s.updateHandler = handlers.NewUpdateHandler()
	s.interactiveHandler = handlers.NewInteractiveHandler(&handlers.InteractiveConfig{
		AllowedOrigins: cfg.CORS.AllowedOrigins,
	}, s.commandRegistry)
	s.installHandler = handlers.NewInstallHandler()

	// Subscription handler
	subStorePath := filepath.Join(filepath.Dir(configPath), "subscriptions.json")
	subStore, subErr := subscription.NewStore(subStorePath)
	if subErr != nil {
		log.Printf("Warning: failed to create subscription store: %v", subErr)
	}
	subFetcher := subscription.NewFetcher()

	// Pass HAPP HWID to the fetcher so happ://crypt5/ subscriptions
	// can authenticate with the server.
	if subStore != nil {
		subFetcher.HAPPHWID = subStore.GetConfig().HAPPHWID
	}

	// Auto-detect a local SOCKS5/HTTP inbound from the Xray config so that
	// subscription fetches are routed through the VPN tunnel (avoids leaks
	// and works around ISP blocking of the subscription host). Falls back
	// silently to direct fetching if no inbound is found.
	if proxyURL := subscription.DetectInboundProxy(cfg.XrayConfigDir); proxyURL != "" {
		if err := subFetcher.SetProxyURL(proxyURL); err != nil {
			log.Printf("Warning: failed to configure fetcher proxy %q: %v", proxyURL, err)
		} else {
			log.Printf("Subscription fetches will route through %s", proxyURL)
		}
	}

	subScheduler := subscription.NewScheduler(subStore, subFetcher)
	subScheduler.SetXrayDir(cfg.XrayConfigDir)
	subScheduler.SetMetricsPort(cfg.MetricsPort)
	subScheduler.SetObservatoryConcurrency(cfg.ObservatoryConcurrency)
	s.subscriptionHandler = handlers.NewSubscriptionHandler(subStore, subFetcher, subScheduler, cfg.XrayConfigDir, cfg.MihomoConfigDir, cfg.AWGConfigDir, cfg.Mode)

	// Wire subscription apply restart to service handler restart
	s.subscriptionHandler.SetRestartFn(func() { s.serviceHandler.RestartService() })

	// Apply proxy_entware mark to outbound generation when enabled
	if cfg.ProxyEntware {
		mark := 255
		s.subscriptionHandler.SetMark(mark)
		subScheduler.SetMark(mark)
	}

	// Apply observatory concurrency to subscription handler
	s.subscriptionHandler.SetObservatoryConcurrency(cfg.ObservatoryConcurrency)

	// AWG handler
	s.awgHandler = handlers.NewAWGHandler(subStore, cfg.AWGConfigDir, cfg)

	// Diagnostics handler (network exit-IP check via fetcher cascade)
	s.diagnosticsHandler = handlers.NewDiagnosticsHandler(subFetcher)

	// Routing categories handler (geosite/geoip .dat file parsing)
	s.routingCategoriesHandler = handlers.NewRoutingCategoriesHandler(cfg.XrayConfigDir)

	// Helper: build tag→remarks from current proxy cache
	buildProxyNames := func() map[string]string {
		proxies := subStore.GetProxies()
		names := make(map[string]string, len(proxies))
		for _, p := range proxies {
			if p.Remarks != "" {
				names[p.Tag] = p.Remarks
			}
		}
		return names
	}

	// Wire scheduler to settings handler for metrics port changes + push proxy names on creation
	s.settingsHandler.SetUpdateMetrics(func(port int) {
		subScheduler.SetMetricsPort(port)
		if s.metricsHandler != nil {
			s.metricsHandler.UpdateProxyNames(buildProxyNames())
		}
	})

	// Wire proxy_entware toggle: update mark on handler+scheduler, regenerate
	// outbounds (so the mark is on disk before xkeen restarts), and run
	// xkeen -pr on/off.
	s.settingsHandler.SetProxyEntwareChange(func(enabled bool) error {
		mark := 0
		if enabled {
			mark = 255
		}
		s.subscriptionHandler.SetMark(mark)
		subScheduler.SetMark(mark)

		// Regenerate 04_outbounds.json with the new mark BEFORE xkeen -pr
		// restarts xray. Without this, xray loads old unmarked outbounds and
		// the iptables `--mark 255 -j RETURN` rule won't match Xray-originated
		// packets, causing a routing loop.
		//
		// When enabling: a regen failure is fatal — abort before xkeen -pr on
		// to avoid the routing loop described above.
		// When disabling: a regen failure is harmless — leftover mark on outbounds
		// is inert once iptables OUTPUT rules are removed, so proceed.
		if err := s.subscriptionHandler.RegenerateOutbounds(); err != nil {
			if enabled {
				return fmt.Errorf("regenerate outbounds before xkeen -pr on: %w", err)
			}
			log.Printf("[proxy-entware] warning: failed to regenerate outbounds on disable: %v", err)
		}

		arg := "off"
		if enabled {
			arg = "on"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cfg.XkeenBinary, "-pr", arg) //nolint:gosec // cfg.XkeenBinary is admin-controlled, arg is a hardcoded literal
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[proxy-entware] xkeen -pr %s failed: %v: %s", arg, err, string(out))
			return fmt.Errorf("xkeen -pr %s: %w", arg, err)
		}
		log.Printf("[proxy-entware] xkeen -pr %s completed", arg)
		return nil
	})

	// Wire observatory concurrency toggle: propagate to handler + scheduler
	s.settingsHandler.SetObservatoryChange(func(enabled bool) {
		s.subscriptionHandler.SetObservatoryConcurrency(enabled)
		subScheduler.SetObservatoryConcurrency(enabled)
	})

	// Wire scheduler OnUpdate: push proxy names after each fetch
	subScheduler.OnUpdate = func() {
		if s.metricsHandler != nil {
			s.metricsHandler.UpdateProxyNames(buildProxyNames())
		}
	}

	// Push initial proxy names from cache into already-created metrics handler (if any).
	// This handles the case where metrics handler was created at startup before subStore loaded cache.
	if s.metricsHandler != nil {
		s.metricsHandler.UpdateProxyNames(buildProxyNames())
	}

	subScheduler.Start()

	// Setup routes
	s.setupRoutes()

	// Log registered routes
	log.Println("Registered routes:")
	_ = s.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		_ = router
		_ = ancestors
		path, _ := route.GetPathTemplate()
		methods, _ := route.GetMethods()
		log.Printf("  %v %s", methods, path)
		return nil
	})

	// Create HTTP server
	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 300 * time.Second, // Increased for long-running commands
		IdleTimeout:  60 * time.Second,
	}

	return s, nil
}

// validateAndResolveBackupPath validates the default backup path and returns a safe backup directory.
// If the default path (parent of XrayConfigDir + /xkeen-ui/backups) is not within AllowedRoots,
// it attempts to find a safe fallback location within one of the AllowedRoots.
func validateAndResolveBackupPath(xrayConfigDir string, allowedRoots []string) (string, error) {
	if len(allowedRoots) == 0 {
		return "", errors.New("allowed roots cannot be empty")
	}

	// Create path validator for checking paths
	validator, err := utils.NewPathValidator(allowedRoots)
	if err != nil {
		return "", fmt.Errorf("failed to create path validator: %w", err)
	}

	// Calculate the default backup directory
	defaultBackupDir := filepath.Join(filepath.Dir(xrayConfigDir), "xkeen-ui", "backups")

	// Validate that the default backup directory is within allowed roots
	if validator.IsAllowed(defaultBackupDir) {
		log.Printf("Backup directory validated: %s", defaultBackupDir)
		return defaultBackupDir, nil
	}

	// Default backup path is not within allowed roots - log warning and find fallback
	log.Printf("WARNING: Default backup directory '%s' is outside AllowedRoots", defaultBackupDir)
	log.Printf("WARNING: XrayConfigDir parent directory is not within allowed roots")

	// Try to find a safe fallback location within one of the allowed roots
	// Priority order:
	// 1. First allowed root that contains "xray" in the path (likely the xray config root)
	// 2. First allowed root that contains "xkeen" in the path
	// 3. First allowed root as last resort

	fallbackDir := ""
	for _, root := range allowedRoots {
		// Try to use a subdirectory within the allowed root
		candidateDir := filepath.Join(root, "xkeen-ui", "backups")
		if validator.IsAllowed(candidateDir) {
			if strings.Contains(root, "xray") && fallbackDir == "" {
				fallbackDir = candidateDir
				break // Prefer xray-containing root
			}
			if strings.Contains(root, "xkeen") && fallbackDir == "" {
				fallbackDir = candidateDir
			}
			if fallbackDir == "" {
				fallbackDir = candidateDir
			}
		}
	}

	if fallbackDir != "" {
		log.Printf("WARNING: Using fallback backup directory: %s", fallbackDir)
		log.Printf("WARNING: To use the default backup location, add the parent directory of XrayConfigDir to AllowedRoots")
		return fallbackDir, nil
	}

	// No valid fallback found - this should not happen if allowedRoots is properly configured
	return "", errors.New("no valid backup directory could be determined within AllowedRoots; please ensure at least one allowed root permits creating a backup subdirectory")
}

// healthCheck returns server health status.
type healthResponse struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

// healthCheck returns server health status.
func (s *Server) healthCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, &healthResponse{OK: true, Status: "healthy", Version: "1.0.0"})
}

type csrfTokenResponse struct {
	OK        bool   `json:"ok"`
	CSRFToken string `json:"csrf_token"`
}

// getCSRFToken returns the CSRF token for the current session.
func (s *Server) getCSRFToken(w http.ResponseWriter, r *http.Request) {
	csrfToken := GetCSRFToken(r.Context())
	writeJSON(w, http.StatusOK, &csrfTokenResponse{OK: true, CSRFToken: csrfToken})
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	log.Printf("Starting HTTP server on :%d", s.cfg.Port)

	if err := s.http.ListenAndServe(); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			return nil // Graceful shutdown
		}
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// Stop gracefully stops the HTTP server.
func (s *Server) Stop() error {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop subscription scheduler
	if s.subscriptionHandler != nil {
		s.subscriptionHandler.Stop()
	}

	// Stop logs handler (tail processes and broadcast goroutine)
	if s.logsHandler != nil {
		s.logsHandler.Close()
	}

	// Stop metrics handler (background workers)
	if s.metricsHandler != nil {
		s.metricsHandler.Close()
	}

	// Stop service handler (background SSE/trigger goroutines)
	if s.serviceHandler != nil {
		s.serviceHandler.Close()
	}

	// Stop session store cleanup goroutine
	if s.sessions != nil {
		s.sessions.Stop()
	}

	// Stop middleware background goroutines
	if s.middleware != nil {
		s.middleware.Stop()
	}

	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	log.Println("Server stopped")
	return nil
}

// IsShuttingDown returns true if server is shutting down.
func (s *Server) IsShuttingDown() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shutdown
}
