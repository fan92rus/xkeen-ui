// Package server provides HTTP server and middleware for XKEEN-GO.
package server

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// contextKey type for context keys to avoid collisions.
type contextKey string

const (
	// SessionKey is the context key for session data.
	SessionKey contextKey = "session"
	// CSRFTokenKey is the context key for CSRF token.
	CSRFTokenKey contextKey = "csrf_token"
)

// SessionManager handles session state.
type SessionManager interface {
	IsValid(sessionToken string) bool
	GetCSRFToken(sessionToken string) string
	CreateSession() (sessionToken, csrfToken string, err error)
	DestroySession(sessionToken string)
}

// SecurityService provides security-related functionality.
type SecurityService interface {
	GenerateToken() (string, error)
	HashPassword(password string) (string, error)
	CheckPassword(password, hash string) bool
}

// RateLimiter tracks failed login attempts.
type RateLimiter struct {
	mu           sync.RWMutex
	attempts     map[string]*attemptInfo
	maxAttempts  int
	lockoutTime  time.Duration
	cleanupTime  time.Duration
	stopCh       chan struct{} // Channel for graceful shutdown
	stopped      bool          // Flag to prevent double stop
	wg           sync.WaitGroup // WaitGroup for goroutine completion
}

type attemptInfo struct {
	count       int
	firstTry    time.Time
	lockedUntil time.Time
}

// NewRateLimiter creates a new RateLimiter and starts the cleanup goroutine.
// The caller must call Stop() to shutdown the cleanup goroutine.
func NewRateLimiter(maxAttempts int, lockoutTime time.Duration) *RateLimiter {
	rl := &RateLimiter{
		attempts:    make(map[string]*attemptInfo),
		maxAttempts: maxAttempts,
		lockoutTime: lockoutTime,
		cleanupTime: 10 * time.Minute,
		stopCh:      make(chan struct{}),
		stopped:     false,
	}

	// Start cleanup goroutine
	rl.wg.Add(1)
	go rl.cleanupLoop()

	return rl
}

// Stop gracefully stops the cleanup goroutine and waits for it to finish.
// It is safe to call Stop multiple times.
func (rl *RateLimiter) Stop() {
	rl.mu.Lock()
	if rl.stopped {
		rl.mu.Unlock()
		return
	}
	rl.stopped = true
	close(rl.stopCh)
	rl.mu.Unlock()

	// Wait for cleanup goroutine to finish
	rl.wg.Wait()
}

// IsLocked returns true if the IP is locked out.
func (rl *RateLimiter) IsLocked(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	info, exists := rl.attempts[ip]
	if !exists {
		return false
	}

	if !info.lockedUntil.IsZero() && time.Now().Before(info.lockedUntil) {
		return true
	}

	return false
}

// RecordAttempt records a failed login attempt.
func (rl *RateLimiter) RecordAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	info, exists := rl.attempts[ip]
	if !exists {
		info = &attemptInfo{
			count:    0,
			firstTry: now,
		}
		rl.attempts[ip] = info
	}

	info.count++

	// Lock out after max attempts
	if info.count >= rl.maxAttempts {
		info.lockedUntil = now.Add(rl.lockoutTime)
	}
}

// Reset clears attempts for an IP (on successful login).
func (rl *RateLimiter) Reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// RemainingLockTime returns the remaining lockout time for an IP.
func (rl *RateLimiter) RemainingLockTime(ip string) time.Duration {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	info, exists := rl.attempts[ip]
	if !exists || info.lockedUntil.IsZero() {
		return 0
	}

	remaining := time.Until(info.lockedUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (rl *RateLimiter) cleanupLoop() {
	defer rl.wg.Done() // Signal completion when goroutine exits

	ticker := time.NewTicker(rl.cleanupTime)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			// Graceful shutdown requested
			return
		case <-ticker.C:
			rl.cleanup()
		}
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, info := range rl.attempts {
		// Remove entries that are no longer locked and older than lockout time
		if !info.lockedUntil.IsZero() && now.After(info.lockedUntil) {
			delete(rl.attempts, ip)
		} else if now.Sub(info.firstTry) > rl.lockoutTime*2 {
			delete(rl.attempts, ip)
		}
	}
}

// Middleware holds dependencies for middleware functions.
type Middleware struct {
	sessions  SessionManager
	security  SecurityService
	rateLimit *RateLimiter
}

// NewMiddleware creates a new Middleware instance.
func NewMiddleware(sessions SessionManager, security SecurityService) *Middleware {
	return &Middleware{
		sessions:  sessions,
		security:  security,
		rateLimit: NewRateLimiter(5, 5*time.Minute), // 5 attempts, 5 min lockout
	}
}

// Stop gracefully stops all background goroutines in the middleware.
func (m *Middleware) Stop() {
	if m.rateLimit != nil {
		m.rateLimit.Stop()
	}
}

// AuthMiddleware checks session validity for protected routes.
func (m *Middleware) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get session cookie
		cookie, err := r.Cookie("session")
		if err != nil {
			if err == http.ErrNoCookie {
				m.handleAuthFailure(w, r, "Authentication required")
				return
			}
			m.handleAuthFailure(w, r, "Invalid session")
			return
		}

		// Validate session
		if !m.sessions.IsValid(cookie.Value) {
			// Clear invalid cookie
			http.SetCookie(w, &http.Cookie{
				Name:     "session",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
			m.handleAuthFailure(w, r, "Session expired")
			return
		}

		// Get CSRF token for this session
		csrfToken := m.sessions.GetCSRFToken(cookie.Value)

		// Add session info to context
		ctx := context.WithValue(r.Context(), SessionKey, cookie.Value)
		ctx = context.WithValue(ctx, CSRFTokenKey, csrfToken)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CSRFMiddleware validates CSRF tokens for mutating requests.
func (m *Middleware) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only check mutating methods
		method := r.Method
		if method != "POST" && method != "PUT" && method != "DELETE" && method != "PATCH" {
			next.ServeHTTP(w, r)
			return
		}

		// Get CSRF token from context (set by AuthMiddleware)
		sessionCSRF, ok := r.Context().Value(CSRFTokenKey).(string)
		if !ok || sessionCSRF == "" {
			m.respondForbidden(w, "CSRF token not found in session")
			return
		}

		// Get CSRF token from request header
		requestCSRF := r.Header.Get("X-CSRF-Token")
		if requestCSRF == "" {
			// Also check form value for compatibility
			requestCSRF = r.FormValue("csrf_token")
		}

		if requestCSRF == "" {
			m.respondForbidden(w, "CSRF token required")
			return
		}

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(sessionCSRF), []byte(requestCSRF)) != 1 {
			m.respondForbidden(w, "Invalid CSRF token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimitMiddleware applies rate limiting to endpoints.
func (m *Middleware) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getRealIP(r)

		// Check if locked
		if m.rateLimit.IsLocked(ip) {
			remaining := m.rateLimit.RemainingLockTime(ip)
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", remaining.Seconds()))
			m.respondTooManyRequests(w, remaining)
			return
		}

		// Store IP in context for handlers to record failed attempts
		ctx := context.WithValue(r.Context(), contextKey("client_ip"), ip)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RecordFailedAttempt records a failed login attempt from the context IP.
func (m *Middleware) RecordFailedAttempt(ctx context.Context) {
	ip, ok := ctx.Value(contextKey("client_ip")).(string)
	if ok && ip != "" {
		m.rateLimit.RecordAttempt(ip)
	}
}

// ResetAttempts clears rate limit for the context IP.
func (m *Middleware) ResetAttempts(ctx context.Context) {
	ip, ok := ctx.Value(contextKey("client_ip")).(string)
	if ok && ip != "" {
		m.rateLimit.Reset(ip)
	}
}

// LoggingMiddleware logs HTTP requests.
func (m *Middleware) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap ResponseWriter to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// Log format: method path status duration ip
		logLine := fmt.Sprintf("%s %s %d %v %s",
			r.Method,
			r.URL.Path,
			wrapped.statusCode,
			duration,
			getRealIP(r),
		)

		// Use standard log for now (can be replaced with structured logging)
		fmt.Printf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), logLine)
	})
}

// CORSMiddleware handles CORS (disabled by default per PRD).
// Enable by setting cfg.CORS.Enabled = true.
func CORSMiddleware(allowedOrigins []string) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If no origins specified, skip CORS
			if len(allowedOrigins) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			allowed := false

			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
			}

			// Handle preflight
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware adds security headers to responses.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// XSS protection
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy (basic)
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://esm.sh https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self' ws: wss: https://esm.sh https://cdn.jsdelivr.net")

		next.ServeHTTP(w, r)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker interface for WebSocket support.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// Flush implements http.Flusher interface for streaming support.
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// getRealIP extracts the real client IP from request.
func getRealIP(r *http.Request) string {
	// Check X-Forwarded-For header (reverse proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	// Remove brackets from IPv6
	ip = strings.TrimPrefix(ip, "[")
	ip = strings.TrimSuffix(ip, "]")

	return ip
}

// Response helpers

// handleAuthFailure handles authentication failures - redirects to login for HTML requests,
// returns JSON for API requests
func (m *Middleware) handleAuthFailure(w http.ResponseWriter, r *http.Request, message string) {
	// Check if this is an API request or HTML request
	path := r.URL.Path
	isAPI := strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/ws")
	accept := r.Header.Get("Accept")
	wantsJSON := strings.Contains(accept, "application/json")
	wantsHTML := strings.Contains(accept, "text/html")

	// API requests always get JSON
	if isAPI || wantsJSON {
		m.respondUnauthorized(w, message)
		return
	}

	// HTML requests or browser navigation get redirect to login
	if wantsHTML || accept == "" || !wantsJSON {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Default to JSON for unknown requests
	m.respondUnauthorized(w, message)
}

func (m *Middleware) respondUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    false,
		"error": message,
	})
}

func (m *Middleware) respondForbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    false,
		"error": message,
	})
}

func (m *Middleware) respondTooManyRequests(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      false,
		"error":   "Too many failed attempts. Please try again later.",
		"retry_after": int(retryAfter.Seconds()),
	})
}

// GetSessionToken extracts session token from context.
func GetSessionToken(ctx context.Context) string {
	token, _ := ctx.Value(SessionKey).(string)
	return token
}

// GetCSRFToken extracts CSRF token from context.
func GetCSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(CSRFTokenKey).(string)
	return token
}

// GetClientIP extracts client IP from context.
func GetClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(contextKey("client_ip")).(string)
	return ip
}
