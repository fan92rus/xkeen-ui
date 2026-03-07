// Package server provides HTTP server functionality for XKEEN-GO.
package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"

	"github.com/user/xkeen-go/internal/config"
	"github.com/user/xkeen-go/internal/handlers"
	"github.com/user/xkeen-go/internal/utils"
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
	configHandler      *handlers.ConfigHandler
	serviceHandler     *handlers.ServiceHandler
	logsHandler        *handlers.LogsHandler
	settingsHandler    *handlers.SettingsHandler
	commandsHandler    *handlers.CommandsHandler
	updateHandler      *handlers.UpdateHandler
	interactiveHandler *handlers.InteractiveHandler

	// Shutdown state
	shutdown bool
	mu       sync.RWMutex
}

// sessionStore implements SessionManager interface.
type sessionStore struct {
	mu             sync.RWMutex
	sessions       map[string]*session
	sessionTimeout time.Duration
	cleanupTime    time.Duration
	stopCh         chan struct{} // Channel for graceful shutdown
	stopped        bool          // Flag to prevent double stop
	wg             sync.WaitGroup // WaitGroup for goroutine completion
}

type session struct {
	username  string
	csrfToken string
	createdAt time.Time
	expiresAt time.Time
}

// securityService implements SecurityService interface.
type securityService struct {
	bcryptCost int
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
	s.configHandler = handlers.NewConfigHandler(cfg.AllowedRoots, backupDir, cfg.XrayConfigDir)
	s.serviceHandler = handlers.NewServiceHandler()
	s.settingsHandler = handlers.NewSettingsHandler(cfg.AllowedRoots, cfg.XrayConfigDir, backupDir)
	s.logsHandler = handlers.NewLogsHandler(handlers.LogsConfig{
		AllowedRoots:   cfg.AllowedRoots,
		AllowedOrigins: cfg.CORS.AllowedOrigins,
		LogFiles: []string{
			"/opt/var/log/xray/access.log",
			"/opt/var/log/xray/error.log",
		},
	})
	s.commandsHandler = handlers.NewCommandsHandler()
	s.updateHandler = handlers.NewUpdateHandler()
	s.interactiveHandler = handlers.NewInteractiveHandler(&handlers.InteractiveConfig{
		AllowedOrigins: cfg.CORS.AllowedOrigins,
	})

	// Setup routes
	s.setupRoutes()

	// Log registered routes
	log.Println("Registered routes:")
	s.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
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
// If the default path (parent of XrayConfigDir + /xkeen-go/backups) is not within AllowedRoots,
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
	defaultBackupDir := filepath.Join(filepath.Dir(xrayConfigDir), "xkeen-go", "backups")

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
		candidateDir := filepath.Join(root, "xkeen-go", "backups")
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

// setupRoutes configures all routes.
func (s *Server) setupRoutes() {
	// Apply global middleware
	s.router.Use(s.middleware.LoggingMiddleware)
	s.router.Use(SecurityHeadersMiddleware)

	// Static files from embedded FS (no auth required)
	staticFS, _ := fs.Sub(s.webFS, "static")
	s.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

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

// healthCheck returns server health status.
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"status":  "healthy",
		"version": "1.0.0",
	})
}

// getCSRFToken returns the CSRF token for the current session.
func (s *Server) getCSRFToken(w http.ResponseWriter, r *http.Request) {
	csrfToken := GetCSRFToken(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"csrf_token": csrfToken,
	})
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

	// Stop logs handler (tail processes and broadcast goroutine)
	if s.logsHandler != nil {
		s.logsHandler.Close()
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

// SessionManager implementation

func newSessionStore(sessionTimeout time.Duration) *sessionStore {
	ss := &sessionStore{
		sessions:       make(map[string]*session),
		sessionTimeout: sessionTimeout,
		cleanupTime:    10 * time.Minute,
		stopCh:         make(chan struct{}),
		stopped:        false,
	}

	// Start cleanup goroutine
	ss.wg.Add(1)
	go ss.cleanupLoop()

	return ss
}

// Stop gracefully stops the cleanup goroutine and waits for it to finish.
// It is safe to call Stop multiple times.
func (ss *sessionStore) Stop() {
	ss.mu.Lock()
	if ss.stopped {
		ss.mu.Unlock()
		return
	}
	ss.stopped = true
	close(ss.stopCh)
	ss.mu.Unlock()

	// Wait for cleanup goroutine to finish
	ss.wg.Wait()
}

func (ss *sessionStore) cleanupLoop() {
	defer ss.wg.Done() // Signal completion when goroutine exits

	ticker := time.NewTicker(ss.cleanupTime)
	defer ticker.Stop()

	for {
		select {
		case <-ss.stopCh:
			// Graceful shutdown requested
			return
		case <-ticker.C:
			ss.cleanup()
		}
	}
}

func (ss *sessionStore) cleanup() {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	now := time.Now()
	for token, sess := range ss.sessions {
		// Remove expired sessions
		if now.After(sess.expiresAt) {
			delete(ss.sessions, token)
		}
	}
}

func (ss *sessionStore) IsValid(sessionToken string) (bool, string) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	sess, exists := ss.sessions[sessionToken]
	if !exists {
		return false, ""
	}

	if time.Now().After(sess.expiresAt) {
		return false, ""
	}

	return true, sess.username
}

func (ss *sessionStore) GetCSRFToken(sessionToken string) string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	sess, exists := ss.sessions[sessionToken]
	if !exists {
		return ""
	}

	return sess.csrfToken
}

func (ss *sessionStore) CreateSession(username string) (sessionToken, csrfToken string, err error) {
	sessionToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate session token: %w", err)
	}

	csrfToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}

	ss.mu.Lock()
	defer ss.mu.Unlock()

	ss.sessions[sessionToken] = &session{
		username:  username,
		csrfToken: csrfToken,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(ss.sessionTimeout),
	}

	return sessionToken, csrfToken, nil
}

func (ss *sessionStore) DestroySession(sessionToken string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.sessions, sessionToken)
}

// SecurityService implementation

func newSecurityService() *securityService {
	return &securityService{
		bcryptCost: 12, // Recommended cost for bcrypt
	}
}

func (ss *securityService) GenerateToken() (string, error) {
	return generateSecureToken(32)
}

func (ss *securityService) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), ss.bcryptCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

func (ss *securityService) CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// generateSecureToken generates a cryptographically secure random token.
// Returns an error if the system's cryptographically secure random number generator fails.
func generateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secure random token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// SetSessionCookie sets the session cookie with secure attributes.
func (s *Server) SetSessionCookie(w http.ResponseWriter, token string) {
	maxAge := s.cfg.Auth.SessionTimeout * 3600 // Convert hours to seconds
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true if using HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	})
}

// SetCSRFTokenCookie sets the CSRF token cookie for client-side JavaScript access.
func (s *Server) SetCSRFTokenCookie(w http.ResponseWriter, token string) {
	maxAge := s.cfg.Auth.SessionTimeout * 3600 // Convert hours to seconds
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    token,
		Path:     "/",
		HttpOnly: false, // Allow JavaScript to read this cookie
		Secure:   false, // Set to true if using HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	})
}

// ClearSessionCookie clears the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	// Also clear CSRF token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
	})
}

// Auth handlers (remain in server package)

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	// Check if already logged in
	if cookie, err := r.Cookie("session"); err == nil {
		if valid, _ := s.sessions.IsValid(cookie.Value); valid {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	// Serve login page from embedded FS
	data, err := fs.ReadFile(s.webFS, "login.html")
	if err != nil {
		http.Error(w, "Login page not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Default username if not provided
	if req.Username == "" {
		req.Username = "admin"
	}

	// Check password from config
	storedHash := s.cfg.Auth.PasswordHash
	if storedHash == "" {
		// If no hash set, use default admin/admin
		var err error
		storedHash, err = s.security.HashPassword("admin")
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
	}

	if req.Username != s.cfg.Auth.Username || !s.security.CheckPassword(req.Password, storedHash) {
		// Record failed attempt
		s.middleware.RecordFailedAttempt(r.Context())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Invalid password",
		})
		return
	}

	// Reset rate limit on successful login
	s.middleware.ResetAttempts(r.Context())

	// Create session
	sessionToken, csrfToken, err := s.sessions.CreateSession(req.Username)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	s.SetSessionCookie(w, sessionToken)

	// Set CSRF token cookie (for client-side access)
	s.SetCSRFTokenCookie(w, csrfToken)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"csrf_token": csrfToken,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		s.sessions.DestroySession(cookie.Value)
	}
	ClearSessionCookie(w)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok": true,
	})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	loggedIn := false
	username := ""

	if cookie, err := r.Cookie("session"); err == nil {
		if valid, user := s.sessions.IsValid(cookie.Value); valid {
			loggedIn = true
			username = user
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"logged_in": loggedIn,
		"user":      username,
	})
}

// changePassword handles password change requests.
// POST /api/auth/change-password
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Invalid request body",
		})
		return
	}

	// Validate input
	if req.CurrentPassword == "" || req.NewPassword == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Current password and new password are required",
		})
		return
	}

	// Validate new password length
	if len(req.NewPassword) < 8 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "New password must be at least 8 characters long",
		})
		return
	}

	// Check if new password is same as current
	if req.CurrentPassword == req.NewPassword {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "New password must be different from current password",
		})
		return
	}

	// Verify current password
	storedHash := s.cfg.Auth.PasswordHash
	if storedHash == "" {
		// If no hash set (shouldn't happen in production), use default
		var err error
		storedHash, err = s.security.HashPassword("admin")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "Internal server error",
			})
			return
		}
	}

	if !s.security.CheckPassword(req.CurrentPassword, storedHash) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Current password is incorrect",
		})
		return
	}

	// Hash new password
	newHash, err := s.security.HashPassword(req.NewPassword)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Failed to hash new password",
		})
		return
	}

	// Update config
	s.cfg.Auth.PasswordHash = newHash

	// Save config to file
	if err := s.cfg.SaveConfig(s.configPath); err != nil {
		log.Printf("Failed to save config: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Failed to save configuration",
		})
		return
	}

	log.Printf("Password changed successfully for user: %s", GetUsername(r.Context()))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "Password changed successfully",
	})
}

func (s *Server) indexPage(w http.ResponseWriter, r *http.Request) {
	// Serve the main web interface from embedded FS
	data, err := fs.ReadFile(s.webFS, "index.html")
	if err != nil {
		http.Error(w, "Page not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
