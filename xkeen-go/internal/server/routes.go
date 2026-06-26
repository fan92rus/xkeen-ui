package server

import (
	"io/fs"
	"net/http"

	"github.com/fan92rus/xkeen-ui/internal/handlers"
)

// setupRoutes configures all routes.
func (s *Server) setupRoutes() {
	// Apply global middleware
	s.router.Use(s.middleware.LoggingMiddleware)
	s.router.Use(SecurityHeadersMiddleware)

	// Static files from embedded FS (no auth required). Wrapped in gzip
	// middleware so the ~750KB frontend bundle (and other large text
	// assets) is transferred ~3x smaller over the router LAN.
	staticFS, _ := fs.Sub(s.webFS, "static")
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	s.router.PathPrefix("/static/").Handler(GzipMiddleware(staticHandler))

	// Login page (no auth)
	s.router.HandleFunc("/login", s.loginPage).Methods("GET")

	// Auth routes (no auth required, but rate limited)
	s.router.Handle("/api/auth/login", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.login))).Methods("POST")
	s.router.Handle("/api/auth/logout", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.logout))).Methods("POST")
	s.router.Handle("/api/auth/status", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.authStatus))).Methods("GET")

	// Protected API routes
	apiRouter := s.router.PathPrefix("/api").Subrouter()
	apiRouter.Use(s.middleware.AuthMiddleware)
	apiRouter.Use(s.middleware.CSRFMiddleware)

	// Register handlers from handlers package
	handlers.RegisterConfigRoutes(apiRouter, s.configHandler)
	handlers.RegisterServiceRoutes(apiRouter, s.serviceHandler)
	handlers.RegisterLogsRoutes(apiRouter, s.logsHandler)
	handlers.RegisterSettingsRoutes(apiRouter, s.settingsHandler)
	handlers.RegisterCommandsRoutes(apiRouter, s.commandsHandler)
	handlers.RegisterUpdateRoutes(apiRouter, s.updateHandler)

	// Subscription routes
	if s.subscriptionHandler != nil {
		handlers.RegisterSubscriptionRoutes(apiRouter, s.subscriptionHandler)
	}

	// AWG installation routes
	handlers.RegisterInstallRoutes(apiRouter, s.installHandler)

	// Metrics routes (optional)
	if s.metricsHandler != nil {
		handlers.RegisterMetricsRoutes(apiRouter, s.metricsHandler)
		handlers.RegisterMetricsWSRoute(s.router, s.metricsHandler, s.middleware.AuthMiddleware)
	}

	// WebSocket routes (auth required, but no CSRF - WebSocket cannot send custom headers)
	handlers.RegisterLogsWSRoute(s.router, s.logsHandler, s.middleware.AuthMiddleware)
	handlers.RegisterInteractiveWSRoute(s.router, s.interactiveHandler, s.middleware.AuthMiddleware)

	// CSRF token endpoint
	apiRouter.HandleFunc("/auth/csrf", s.getCSRFToken).Methods("GET")

	// Password change endpoint
	apiRouter.HandleFunc("/auth/change-password", s.changePassword).Methods("POST")

	// Main page (protected)
	s.router.Handle("/", s.middleware.AuthMiddleware(http.HandlerFunc(s.indexPage))).Methods("GET")

	// Health check (no auth)
	s.router.HandleFunc("/health", s.healthCheck).Methods("GET")
}
