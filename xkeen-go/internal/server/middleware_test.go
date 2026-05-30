package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockSessionManager implements SessionManager for testing.
type mockSessionManager struct {
	sessions  map[string]string // sessionToken -> csrfToken
	validCalls int
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		sessions: make(map[string]string),
	}
}

func (m *mockSessionManager) IsValid(sessionToken string) bool {
	m.validCalls++
	_, ok := m.sessions[sessionToken]
	return ok
}

func (m *mockSessionManager) GetCSRFToken(sessionToken string) string {
	return m.sessions[sessionToken]
}

func (m *mockSessionManager) CreateSession() (string, string, error) {
	token := "session-token-123"
	csrf := "csrf-token-456"
	m.sessions[token] = csrf
	return token, csrf, nil
}

func (m *mockSessionManager) DestroySession(sessionToken string) {
	delete(m.sessions, sessionToken)
}

// mockSecurityService implements SecurityService for testing.
type mockSecurityService struct{}

func (m *mockSecurityService) GenerateToken() (string, error) {
	return "generated-token", nil
}

func (m *mockSecurityService) HashPassword(password string) (string, error) {
	return "hashed-" + password, nil
}

func (m *mockSecurityService) CheckPassword(password, hash string) bool {
	return hash == "hashed-"+password
}

// Helper to create a Middleware with mocks for testing.
func newTestMiddleware() (*Middleware, *mockSessionManager) {
	sessions := newMockSessionManager()
	security := &mockSecurityService{}
	mw := NewMiddleware(sessions, security)
	return mw, sessions
}

// --- RateLimiter Tests ---

func TestRateLimiter_NotLockedByDefault(t *testing.T) {
	rl := NewRateLimiter(5, 5*time.Minute)
	defer rl.Stop()

	if rl.IsLocked("192.168.1.1") {
		t.Error("IP should not be locked by default")
	}
}

func TestRateLimiter_LockedAfterMaxAttempts(t *testing.T) {
	rl := NewRateLimiter(3, 5*time.Minute)
	defer rl.Stop()

	ip := "10.0.0.1"
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip) // 3rd attempt = max

	if !rl.IsLocked(ip) {
		t.Error("IP should be locked after max attempts")
	}
}

func TestRateLimiter_NotLockedBeforeMax(t *testing.T) {
	rl := NewRateLimiter(5, 5*time.Minute)
	defer rl.Stop()

	ip := "10.0.0.2"
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip) // 4 of 5

	if rl.IsLocked(ip) {
		t.Error("IP should not be locked before reaching max attempts")
	}
}

func TestRateLimiter_ResetClearsAttempts(t *testing.T) {
	rl := NewRateLimiter(3, 5*time.Minute)
	defer rl.Stop()

	ip := "10.0.0.3"
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip)
	rl.RecordAttempt(ip)
	rl.Reset(ip)

	if rl.IsLocked(ip) {
		t.Error("IP should not be locked after reset")
	}
}

func TestRateLimiter_DifferentIPsTrackedSeparately(t *testing.T) {
	rl := NewRateLimiter(2, 5*time.Minute)
	defer rl.Stop()

	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"

	rl.RecordAttempt(ip1)
	rl.RecordAttempt(ip1) // locked

	if rl.IsLocked(ip2) {
		t.Error("Different IP should not be affected")
	}
	if !rl.IsLocked(ip1) {
		t.Error("Original IP should be locked")
	}
}

func TestRateLimiter_RemainingLockTime(t *testing.T) {
	lockout := 10 * time.Minute
	rl := NewRateLimiter(1, lockout)
	defer rl.Stop()

	ip := "10.0.0.4"
	rl.RecordAttempt(ip) // max=1, so immediately locked

	remaining := rl.RemainingLockTime(ip)
	if remaining <= 0 {
		t.Error("Expected positive remaining lock time")
	}
	if remaining > lockout {
		t.Errorf("Remaining time %v should not exceed lockout %v", remaining, lockout)
	}
}

func TestRateLimiter_RemainingLockTimeZeroIfNotLocked(t *testing.T) {
	rl := NewRateLimiter(5, 5*time.Minute)
	defer rl.Stop()

	remaining := rl.RemainingLockTime("1.2.3.4")
	if remaining != 0 {
		t.Errorf("Expected 0 remaining time for untracked IP, got %v", remaining)
	}
}

func TestRateLimiter_StopIsIdempotent(t *testing.T) {
	rl := NewRateLimiter(5, 5*time.Minute)
	rl.Stop()
	rl.Stop() // should not panic
}

// --- SecurityHeadersMiddleware Tests ---

func TestSecurityHeadersMiddleware_SetsHeaders(t *testing.T) {
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	tests := []struct {
		header string
		want   string
	}{
		{"X-Frame-Options", "DENY"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		got := rec.Header().Get(tt.header)
		if got != tt.want {
			t.Errorf("Header %s: got %q, want %q", tt.header, got, tt.want)
		}
	}

	// CSP should contain key directives
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP missing default-src: %s", csp)
	}
}

func TestSecurityHeadersMiddleware_PassesThrough(t *testing.T) {
	called := false
	handler := SecurityHeadersMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Next handler was not called")
	}
}

// --- AuthMiddleware Tests ---

func TestAuthMiddleware_RejectsNoCookie(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_RejectsInvalidSession(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "invalid-token"})
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddleware_AcceptsValidSession(t *testing.T) {
	mw, sessions := newTestMiddleware()
	defer mw.Stop()

	sessions.sessions["valid-token"] = "csrf-abc"

	called := false
	handler := mw.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Verify context values
		session := GetSessionToken(r.Context())
		csrf := GetCSRFToken(r.Context())
		if session != "valid-token" {
			t.Errorf("Session token: got %q, want %q", session, "valid-token")
		}
		if csrf != "csrf-abc" {
			t.Errorf("CSRF token: got %q, want %q", csrf, "csrf-abc")
		}
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "valid-token"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler was not called")
	}
}

func TestAuthMiddleware_ClearsInvalidCookie(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "bogus"})
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should set cookie with MaxAge=-1 to clear it
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session" && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Error("Expected session cookie to be cleared (MaxAge=-1)")
	}
}

func TestAuthMiddleware_RedirectsHTMLRequests(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected 303 redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/login" {
		t.Errorf("Expected redirect to /login, got %s", loc)
	}
}

// --- CSRFMiddleware Tests ---

func TestCSRFMiddleware_SkipsGETRequests(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	called := false
	handler := mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("GET request should pass through")
	}
}

func TestCSRFMiddleware_RejectsPOSTWithoutToken(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	// Need CSRF in context (set by AuthMiddleware)
	ctx := context.WithValue(context.Background(), CSRFTokenKey, "expected-csrf")
	req := httptest.NewRequest("POST", "/api/test", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_RejectsWrongToken(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	ctx := context.WithValue(context.Background(), CSRFTokenKey, "expected-csrf")
	req := httptest.NewRequest("POST", "/api/test", nil).WithContext(ctx)
	req.Header.Set("X-CSRF-Token", "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403, got %d", rec.Code)
	}
}

func TestCSRFMiddleware_AcceptsValidToken(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	called := false
	handler := mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	ctx := context.WithValue(context.Background(), CSRFTokenKey, "correct-token")
	req := httptest.NewRequest("POST", "/api/test", nil).WithContext(ctx)
	req.Header.Set("X-CSRF-Token", "correct-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Valid CSRF token should pass through")
	}
}

func TestCSRFMiddleware_AcceptsFormValue(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	called := false
	handler := mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	ctx := context.WithValue(context.Background(), CSRFTokenKey, "form-csrf")
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader("csrf_token=form-csrf"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("CSRF token from form value should be accepted")
	}
}

func TestCSRFMiddleware_NoCSRFinContext(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	}))

	req := httptest.NewRequest("POST", "/api/test", nil)
	// No CSRFTokenKey in context
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 without CSRF in context, got %d", rec.Code)
	}
}

// --- RateLimitMiddleware Tests ---

func TestRateLimitMiddleware_PassesWhenNotLocked(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	called := false
	handler := mw.RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Check that IP is in context
		ip := GetClientIP(r.Context())
		if ip == "" {
			t.Error("Expected client IP in context")
		}
	}))

	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Request should pass when not locked")
	}
}

func TestRateLimitMiddleware_BlocksWhenLocked(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	// Lock the IP manually
	mw.rateLimit.RecordAttempt("192.168.1.200")
	mw.rateLimit.RecordAttempt("192.168.1.200")
	mw.rateLimit.RecordAttempt("192.168.1.200")
	mw.rateLimit.RecordAttempt("192.168.1.200")
	mw.rateLimit.RecordAttempt("192.168.1.200") // 5th = locked

	handler := mw.RateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler when locked")
	}))

	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "192.168.1.200:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", rec.Code)
	}

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header")
	}
}

// --- CORSMiddleware Tests ---

func TestCORSMiddleware_SetsHeadersForAllowedOrigin(t *testing.T) {
	called := false
	handler := CORSMiddleware([]string{"http://localhost:3000"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler was not called")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("CORS origin: got %q, want %q", got, "http://localhost:3000")
	}
}

func TestCORSMiddleware_Wildcard(t *testing.T) {
	handler := CORSMiddleware([]string{"*"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://any-origin.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://any-origin.com" {
		t.Errorf("Wildcard CORS: got %q", got)
	}
}

func TestCORSMiddleware_HandlesPreflight(t *testing.T) {
	handler := CORSMiddleware([]string{"http://localhost:3000"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("Should not call handler for OPTIONS")
		}),
	)

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Preflight: expected 204, got %d", rec.Code)
	}
}

func TestCORSMiddleware_SkipsWhenNoOrigins(t *testing.T) {
	called := false
	handler := CORSMiddleware([]string{})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when no origins configured")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("No CORS headers expected when origins empty, got %q", got)
	}
}

func TestCORSMiddleware_BlocksDisallowedOrigin(t *testing.T) {
	handler := CORSMiddleware([]string{"http://localhost:3000"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Should not set CORS headers for disallowed origin, got %q", got)
	}
}

// --- getRealIP Tests ---

func TestGetRealIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")

	ip := getRealIP(req)
	if ip != "1.2.3.4" {
		t.Errorf("Expected first IP from X-Forwarded-For, got %q", ip)
	}
}

func TestGetRealIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Real-IP", "9.8.7.6")

	ip := getRealIP(req)
	if ip != "9.8.7.6" {
		t.Errorf("Expected X-Real-IP, got %q", ip)
	}
}

func TestGetRealIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:54321"

	ip := getRealIP(req)
	if ip != "10.0.0.1" {
		t.Errorf("Expected IP from RemoteAddr, got %q", ip)
	}
}

func TestGetRealIP_PrefersForwardedOverRemote(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	req.RemoteAddr = "10.0.0.1:54321"

	ip := getRealIP(req)
	if ip != "1.1.1.1" {
		t.Errorf("Should prefer X-Forwarded-For over RemoteAddr, got %q", ip)
	}
}

// --- Middleware Stop ---

func TestMiddleware_StopIdempotent(t *testing.T) {
	mw, _ := newTestMiddleware()
	mw.Stop()
	mw.Stop() // should not panic
}

// --- RecordFailedAttempt / ResetAttempts ---

func TestRecordFailedAttempt(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	ctx := context.WithValue(context.Background(), contextKey("client_ip"), "1.2.3.4")
	for i := 0; i < 5; i++ {
		mw.RecordFailedAttempt(ctx)
	}

	if !mw.rateLimit.IsLocked("1.2.3.4") {
		t.Error("IP should be locked after 5 failed attempts")
	}
}

func TestResetAttempts(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	ctx := context.WithValue(context.Background(), contextKey("client_ip"), "5.6.7.8")
	for i := 0; i < 5; i++ {
		mw.RecordFailedAttempt(ctx)
	}

	if !mw.rateLimit.IsLocked("5.6.7.8") {
		t.Fatal("IP should be locked")
	}

	mw.ResetAttempts(ctx)

	if mw.rateLimit.IsLocked("5.6.7.8") {
		t.Error("IP should be unlocked after reset")
	}
}

// --- Full chain test (Auth + CSRF) ---

func TestFullChain_ValidSessionAndCSRF(t *testing.T) {
	mw, sessions := newTestMiddleware()
	defer mw.Stop()

	sessions.sessions["good-session"] = "good-csrf"

	called := false
	handler := mw.AuthMiddleware(mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})))

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "good-session"})
	req.Header.Set("X-CSRF-Token", "good-csrf")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Full chain should pass with valid session + CSRF")
	}
}

func TestFullChain_ValidSessionBadCSRF(t *testing.T) {
	mw, sessions := newTestMiddleware()
	defer mw.Stop()

	sessions.sessions["good-session"] = "good-csrf"

	handler := mw.AuthMiddleware(mw.CSRFMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler")
	})))

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "good-session"})
	req.Header.Set("X-CSRF-Token", "bad-csrf")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 for bad CSRF, got %d", rec.Code)
	}
}

// --- responseWriter Tests ---

func TestResponseWriter_CapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusNotFound)

	if rw.statusCode != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", rw.statusCode)
	}
}

func TestResponseWriter_DefaultStatusOK(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	rw.Write([]byte("hello"))

	if rw.statusCode != http.StatusOK {
		t.Errorf("Expected 200 after Write without WriteHeader, got %d", rw.statusCode)
	}
}

func TestResponseWriter_WriteHeaderOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusBadRequest)
	rw.WriteHeader(http.StatusNotFound) // should be ignored

	if rw.statusCode != http.StatusBadRequest {
		t.Errorf("Expected first WriteHeader status 400, got %d", rw.statusCode)
	}
}

// --- Helper: parse JSON response body ---

func parseJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}
	return result
}

func TestAuthMiddleware_ErrorMessage(t *testing.T) {
	mw, _ := newTestMiddleware()
	defer mw.Stop()

	handler := mw.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := parseJSONBody(t, rec)
	if body["error"] == nil {
		t.Error("Expected error message in response body")
	}
}
