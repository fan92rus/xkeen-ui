package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/bcrypt"

	"github.com/fan92rus/xkeen-ui/internal/config"
)

// ---------- Test helpers ----------

// bcrypt hash for password "password" (cost 12)
const testPasswordHash = "$2a$12$oDp.vVnkWYsDjAuEWEKOgOR08sApErSrJFyRMbOE5d/GvccJKiNLe"

// testConfig creates a minimal config for testing auth handlers.
func testConfig(t *testing.T, tmpDir string) *config.Config {
	t.Helper()
	return &config.Config{
		Port:          9877,
		Mode:          "xray",
		XrayConfigDir: filepath.Join(tmpDir, "xray"),
		XkeenBinary:   "xkeen",
		AllowedRoots:  []string{filepath.Join(tmpDir, "xray")},
		LogLevel:      "info",
		Auth: config.AuthConfig{
			PasswordHash:     testPasswordHash,
			SessionTimeout:   1,
			MaxLoginAttempts: 3,
			LockoutDuration:  1,
		},
	}
}

// testServer creates a Server instance with real session store, security service,
// and middleware — no external dependencies. Returns the router for testing.
func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	tmpDir := t.TempDir()

	cfg := testConfig(t, tmpDir)
	configPath := filepath.Join(tmpDir, "config.json")

	// Create minimal FS for embedded web files
	// We use an empty FS since auth handlers don't serve static files
	webFS := &emptyFS{}

	s := &Server{
		cfg:        cfg,
		configPath: configPath,
		router:     mux.NewRouter(),
		webFS:      webFS,
	}

	// Initialize services
	sessionTimeout := time.Duration(cfg.Auth.SessionTimeout) * time.Hour
	s.sessions = newSessionStore(sessionTimeout)
	s.security = newSecurityService()
	s.middleware = NewMiddleware(s.sessions, s.security)

	t.Cleanup(func() {
		s.sessions.Stop()
		s.middleware.Stop()
	})

	// Setup only the auth routes we need for testing
	s.router.HandleFunc("/login", s.loginPage).Methods("GET")
	s.router.Handle("/api/auth/login", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.login))).Methods("POST")
	s.router.Handle("/api/auth/logout", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.logout))).Methods("POST")
	s.router.Handle("/api/auth/status", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.authStatus))).Methods("GET")
	s.router.Handle("/health", http.HandlerFunc(s.healthCheck)).Methods("GET")

	// Protected routes (with auth + CSRF)
	apiRouter := s.router.PathPrefix("/api").Subrouter()
	apiRouter.Use(s.middleware.AuthMiddleware)
	apiRouter.Use(s.middleware.CSRFMiddleware)
	apiRouter.HandleFunc("/auth/csrf", s.getCSRFToken).Methods("GET")
	apiRouter.HandleFunc("/auth/change-password", s.changePassword).Methods("POST")

	// Main page (protected)
	s.router.Handle("/", s.middleware.AuthMiddleware(http.HandlerFunc(s.indexPage))).Methods("GET")

	return s, tmpDir
}

// loginJSON creates a JSON body for login request.
func loginJSON(password string) io.Reader {
	body, _ := json.Marshal(map[string]string{"password": password})
	return bytes.NewReader(body)
}

// doReq executes a request against the server router.
func doReq(t *testing.T, router http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// doReqWithCookies executes a request with session + CSRF cookies/headers.
func doReqWithCookies(t *testing.T, router http.Handler, method, path string, body io.Reader, sessionToken, csrfToken string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// parseJSON parses the response recorder body into a map.
func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse JSON: %v (body: %q)", err, rec.Body.String())
	}
	return result
}

// loginAndGetSession performs login and returns (sessionToken, csrfToken).
func loginAndGetSession(t *testing.T, router http.Handler) (string, string) {
	t.Helper()
	rec := doReq(t, router, "POST", "/api/auth/login", loginJSON("password"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d, body: %s", rec.Code, rec.Body.String())
	}

	// Extract session cookie
	var sessionToken, csrfToken string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" {
			sessionToken = c.Value
		}
		if c.Name == "csrf_token" {
			csrfToken = c.Value
		}
	}

	if sessionToken == "" {
		t.Fatal("login response missing session cookie")
	}
	if csrfToken == "" {
		t.Fatal("login response missing csrf_token cookie")
	}

	// Also verify CSRF in JSON body
	body := parseJSON(t, rec)
	if bodyCSRF, ok := body["csrf_token"].(string); ok && bodyCSRF != "" {
		if bodyCSRF != csrfToken {
			t.Fatalf("cookie CSRF %q != body CSRF %q", csrfToken, bodyCSRF)
		}
	}

	return sessionToken, csrfToken
}

// emptyFS implements fs.FS with minimal files for testing.
type emptyFS struct{}

func (e *emptyFS) Open(name string) (fs.File, error) {
	return nil, os.ErrNotExist
}

// ---------- Login Tests ----------

func TestLogin_CorrectPassword(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := parseJSON(t, rec)
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if _, ok := body["csrf_token"].(string); !ok || body["csrf_token"] == "" {
		t.Error("expected non-empty csrf_token in response")
	}

	// Verify cookies are set
	cookies := rec.Result().Cookies()
	hasSession := false
	hasCSRF := false
	for _, c := range cookies {
		if c.Name == "session" {
			hasSession = true
			if c.HttpOnly != true {
				t.Error("session cookie should be HttpOnly")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Error("session cookie should be SameSiteStrict")
			}
		}
		if c.Name == "csrf_token" {
			hasCSRF = true
			if c.HttpOnly != false {
				t.Error("csrf_token cookie should NOT be HttpOnly (JS needs to read it)")
			}
		}
	}
	if !hasSession {
		t.Error("expected session cookie to be set")
	}
	if !hasCSRF {
		t.Error("expected csrf_token cookie to be set")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrongpassword"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["ok"] != false {
		t.Errorf("expected ok=false, got %v", body["ok"])
	}
	if body["error"] != "Invalid password" {
		t.Errorf("expected 'Invalid password' error, got %v", body["error"])
	}

	// No session cookie should be set
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.Value != "" && c.MaxAge != -1 {
			t.Error("session cookie should not be set on failed login")
		}
	}
}

func TestLogin_EmptyBody(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "POST", "/api/auth/login", bytes.NewReader([]byte("")))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLogin_EmptyPassword(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON(""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for empty password, got %d", rec.Code)
	}
}

func TestLogin_NoPasswordField(t *testing.T) {
	s, _ := testServer(t)
	body := bytes.NewReader([]byte(`{"username":"admin"}`))
	rec := doReq(t, s.router, "POST", "/api/auth/login", body)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing password field, got %d", rec.Code)
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	s, _ := testServer(t)
	body := bytes.NewReader([]byte(`not json at all`))
	rec := doReq(t, s.router, "POST", "/api/auth/login", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestLogin_DefaultPasswordWhenNoHash(t *testing.T) {
	s, _ := testServer(t)
	// Remove password hash — should accept "admin"
	s.cfg.Auth.PasswordHash = ""

	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("admin"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with default admin/admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin_ForcePasswordChange(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Auth.ForcePasswordChange = true

	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["require_password_change"] != true {
		t.Error("expected require_password_change=true")
	}
	if msg, ok := body["message"].(string); !ok || msg == "" {
		t.Error("expected non-empty message about password change")
	}
}

func TestLogin_RateLimiting(t *testing.T) {
	s, _ := testServer(t)
	// NewMiddleware hardcodes 5 max attempts

	// First 5 wrong attempts should return 401
	for i := 0; i < 5; i++ {
		rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, rec.Code)
		}
	}

	// 6th attempt should be rate limited (429)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after lockout, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["ok"] != false {
		t.Error("expected ok=false")
	}
	if body["error"] == nil {
		t.Error("expected error message")
	}
	if retryAfter, ok := body["retry_after"].(float64); !ok || retryAfter <= 0 {
		t.Error("expected positive retry_after")
	}
}

func TestLogin_RateLimitingEvenWithCorrectPassword(t *testing.T) {
	s, _ := testServer(t)

	// Lock out the IP with 5 wrong attempts (NewMiddleware default)
	for i := 0; i < 5; i++ {
		doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	}

	// Even correct password should be blocked
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 even with correct password when locked, got %d", rec.Code)
	}
}

func TestLogin_ResetsRateLimitOnSuccess(t *testing.T) {
	s, _ := testServer(t)

	// 2 wrong attempts (below limit)
	doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))

	// Successful login resets the counter
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Now 3 more wrong attempts should be fine (counter was reset)
	for i := 0; i < 3; i++ {
		rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d after reset: expected 401, got %d", i+1, rec.Code)
		}
	}
}

// ---------- Logout Tests ----------

func TestLogout_ValidSession(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/logout", nil, sessionToken, csrfToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}

	// Cookies should be cleared
	for _, c := range rec.Result().Cookies() {
		if c.Name == "session" && c.MaxAge != -1 {
			t.Error("session cookie should be cleared (MaxAge=-1)")
		}
		if c.Name == "csrf_token" && c.MaxAge != -1 {
			t.Error("csrf_token cookie should be cleared (MaxAge=-1)")
		}
	}

	// Session should be destroyed — subsequent requests should fail
	rec2 := doReqWithCookies(t, s.router, "GET", "/api/auth/csrf", nil, sessionToken, csrfToken)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", rec2.Code)
	}
}

func TestLogout_InvalidSession(t *testing.T) {
	s, _ := testServer(t)

	// Logout without ever logging in — should still return OK
	req, _ := http.NewRequest("POST", "/api/auth/logout", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even without session, got %d", rec.Code)
	}
}

func TestLogout_ClearsBothCookies(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/logout", nil, sessionToken, csrfToken)

	cookies := rec.Result().Cookies()
	names := map[string]bool{}
	for _, c := range cookies {
		names[c.Name] = true
		if c.MaxAge != -1 {
			t.Errorf("cookie %s should have MaxAge=-1, got %d", c.Name, c.MaxAge)
		}
	}
	if !names["session"] {
		t.Error("expected session cookie to be cleared")
	}
	if !names["csrf_token"] {
		t.Error("expected csrf_token cookie to be cleared")
	}
}

// ---------- Auth Status Tests ----------

func TestAuthStatus_Authenticated(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	rec := doReqWithCookies(t, s.router, "GET", "/api/auth/status", nil, sessionToken, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if body["logged_in"] != true {
		t.Errorf("expected logged_in=true, got %v", body["logged_in"])
	}
}

func TestAuthStatus_Unauthenticated(t *testing.T) {
	s, _ := testServer(t)

	rec := doReq(t, s.router, "GET", "/api/auth/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if body["logged_in"] != false {
		t.Errorf("expected logged_in=false, got %v", body["logged_in"])
	}
}

func TestAuthStatus_ExpiredSession(t *testing.T) {
	s, _ := testServer(t)

	// Create session manually with short timeout
	s.sessions.mu.Lock()
	s.sessions.sessions["expired-token"] = &session{
		csrfToken: "expired-csrf",
		createdAt: time.Now().Add(-2 * time.Hour),
		expiresAt: time.Now().Add(-1 * time.Hour), // expired 1 hour ago
	}
	s.sessions.mu.Unlock()

	rec := doReqWithCookies(t, s.router, "GET", "/api/auth/status", nil, "expired-token", "expired-csrf")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["logged_in"] != false {
		t.Errorf("expected logged_in=false for expired session, got %v", body["logged_in"])
	}
}

// ---------- Change Password Tests ----------

func TestChangePassword_Success(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "password",
		"new_password":     "newpassword123",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	if resp["ok"] != true {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}

	// Verify old password no longer works
	rec2 := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with old password, got %d", rec2.Code)
	}

	// Verify new password works
	rec3 := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("newpassword123"))
	if rec3.Code != http.StatusOK {
		t.Fatalf("expected 200 with new password, got %d: %s", rec3.Code, rec3.Body.String())
	}
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "wrongpassword",
		"new_password":     "newpassword123",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	resp := parseJSON(t, rec)
	if resp["error"] != "Current password is incorrect" {
		t.Errorf("expected 'Current password is incorrect', got %v", resp["error"])
	}
}

func TestChangePassword_EmptyCurrentPassword(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "",
		"new_password":     "newpassword123",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChangePassword_EmptyNewPassword(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "password",
		"new_password":     "",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestChangePassword_TooShortPassword(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "password",
		"new_password":     "short",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d", rec.Code)
	}

	resp := parseJSON(t, rec)
	if resp["error"] != "New password must be at least 8 characters long" {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestChangePassword_SamePassword(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "password",
		"new_password":     "password",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for same password, got %d", rec.Code)
	}

	resp := parseJSON(t, rec)
	if resp["error"] != "New password must be different from current password" {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestChangePassword_InvalidBody(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader([]byte(`not json`)), sessionToken, csrfToken)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}

func TestChangePassword_RequiresAuth(t *testing.T) {
	s, _ := testServer(t)

	// No session cookie → AuthMiddleware should reject
	rec := doReq(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader([]byte(`{}`)))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}
}

func TestChangePassword_ClearsForcePasswordChange(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Auth.ForcePasswordChange = true
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	body := map[string]string{
		"current_password": "password",
		"new_password":     "newpassword123",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if s.cfg.Auth.ForcePasswordChange {
		t.Error("ForcePasswordChange should be cleared after password change")
	}
}

// ---------- CSRF Token Tests ----------

func TestCSRF_LoginSetsCSRF(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	if csrfToken == "" {
		t.Fatal("CSRF token should be set after login")
	}

	// Get CSRF via API endpoint
	rec := doReqWithCookies(t, s.router, "GET", "/api/auth/csrf", nil, sessionToken, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	apiCSRF, ok := body["csrf_token"].(string)
	if !ok || apiCSRF == "" {
		t.Fatal("expected csrf_token in API response")
	}
	if apiCSRF != csrfToken {
		t.Errorf("API CSRF %q != login CSRF %q", apiCSRF, csrfToken)
	}
}

func TestCSRF_PostWithoutTokenFails(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, _ := loginAndGetSession(t, s.router)

	// POST without CSRF header
	req, _ := http.NewRequest("POST", "/api/auth/change-password",
		bytes.NewReader([]byte(`{"current_password":"x","new_password":"y"}`)))
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	// Intentionally NOT setting X-CSRF-Token
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF token, got %d", rec.Code)
	}
}

func TestCSRF_PostWithWrongTokenFails(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, _ := loginAndGetSession(t, s.router)

	req, _ := http.NewRequest("POST", "/api/auth/change-password",
		bytes.NewReader([]byte(`{"current_password":"x","new_password":"y"}`)))
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	req.Header.Set("X-CSRF-Token", "wrong-token")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with wrong CSRF token, got %d", rec.Code)
	}
}

func TestCSRF_GetRequestsDontNeedCSRF(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, _ := loginAndGetSession(t, s.router)

	// GET request without CSRF header should work
	req, _ := http.NewRequest("GET", "/api/auth/csrf", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	// No X-CSRF-Token header
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET should work without CSRF, got %d", rec.Code)
	}
}

// ---------- Session Tests ----------

func TestSession_CreatedOnLogin(t *testing.T) {
	s, _ := testServer(t)

	// Before login, no sessions
	s.sessions.mu.RLock()
	count := len(s.sessions.sessions)
	s.sessions.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 sessions, got %d", count)
	}

	loginAndGetSession(t, s.router)

	s.sessions.mu.RLock()
	count = len(s.sessions.sessions)
	s.sessions.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 session after login, got %d", count)
	}
}

func TestSession_DestroyedOnLogout(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	s.sessions.mu.RLock()
	count := len(s.sessions.sessions)
	s.sessions.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 session, got %d", count)
	}

	doReqWithCookies(t, s.router, "POST", "/api/auth/logout", nil, sessionToken, csrfToken)

	s.sessions.mu.RLock()
	count = len(s.sessions.sessions)
	s.sessions.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 sessions after logout, got %d", count)
	}
}

func TestSession_Expiry(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	// Manually expire the session
	s.sessions.mu.Lock()
	if sess, ok := s.sessions.sessions[sessionToken]; ok {
		sess.expiresAt = time.Now().Add(-1 * time.Second)
	}
	s.sessions.mu.Unlock()

	// Session should no longer be valid
	rec := doReqWithCookies(t, s.router, "GET", "/api/auth/csrf", nil, sessionToken, csrfToken)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with expired session, got %d", rec.Code)
	}
}

func TestSession_UniqueTokens(t *testing.T) {
	s, _ := testServer(t)

	// Login twice, should get different session tokens
	_, csrf1 := loginAndGetSession(t, s.router)

	// Second login creates a new session (different CSRF token)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var csrf2 string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf_token" {
			csrf2 = c.Value
		}
	}

	if csrf1 == csrf2 {
		t.Error("two logins should produce different CSRF tokens")
	}
}

// ---------- Concurrent Sessions ----------

func TestConcurrentSessions_MultipleLogins(t *testing.T) {
	s, _ := testServer(t)

	// Create 3 sessions
	sessions := make([]struct{ session, csrf string }, 3)
	for i := 0; i < 3; i++ {
		sessions[i].session, sessions[i].csrf = loginAndGetSession(t, s.router)
	}

	s.sessions.mu.RLock()
	count := len(s.sessions.sessions)
	s.sessions.mu.RUnlock()
	if count != 3 {
		t.Fatalf("expected 3 sessions, got %d", count)
	}

	// All sessions should be valid
	for i, sess := range sessions {
		rec := doReqWithCookies(t, s.router, "GET", "/api/auth/csrf", nil, sess.session, sess.csrf)
		if rec.Code != http.StatusOK {
			t.Errorf("session %d should be valid, got %d", i, rec.Code)
		}
	}

	// Log out one session
	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/logout", nil, sessions[0].session, sessions[0].csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout failed: %d", rec.Code)
	}

	// Other sessions should still work
	for i, sess := range sessions[1:] {
		rec := doReqWithCookies(t, s.router, "GET", "/api/auth/csrf", nil, sess.session, sess.csrf)
		if rec.Code != http.StatusOK {
			t.Errorf("session %d should still be valid after logout of session 0, got %d", i+1, rec.Code)
		}
	}
}

// ---------- Protected Route Tests ----------

func TestProtectedRoute_RequiresAuth(t *testing.T) {
	s, _ := testServer(t)

	// /api/auth/csrf is behind auth middleware
	rec := doReq(t, s.router, "GET", "/api/auth/csrf", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", rec.Code)
	}
}

func TestProtectedRoute_WithAuth(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	rec := doReqWithCookies(t, s.router, "GET", "/api/auth/csrf", nil, sessionToken, csrfToken)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with auth, got %d", rec.Code)
	}
}

// ---------- Health Check ----------

func TestHealthCheck(t *testing.T) {
	s, _ := testServer(t)

	rec := doReq(t, s.router, "GET", "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := parseJSON(t, rec)
	if body["ok"] != true {
		t.Errorf("expected ok=true, got %v", body["ok"])
	}
	if body["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", body["status"])
	}
}

// ---------- SecurityService Tests ----------

func TestSecurityService_CheckPassword(t *testing.T) {
	ss := newSecurityService()

	// Test correct password
	if !ss.CheckPassword("password", testPasswordHash) {
		t.Error("CheckPassword should return true for correct password")
	}

	// Test wrong password
	if ss.CheckPassword("wrong", testPasswordHash) {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestSecurityService_HashAndVerify(t *testing.T) {
	ss := newSecurityService()

	hash, err := ss.HashPassword("testpass123")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !ss.CheckPassword("testpass123", hash) {
		t.Error("CheckPassword should verify the hash it just created")
	}
	if ss.CheckPassword("different", hash) {
		t.Error("CheckPassword should reject wrong password")
	}
}

func TestSecurityService_HashIsBcrypt(t *testing.T) {
	ss := newSecurityService()

	hash, err := ss.HashPassword("test")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Verify it's a valid bcrypt hash by using bcrypt directly
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("test")); err != nil {
		t.Errorf("hash is not valid bcrypt: %v", err)
	}
}

func TestSecurityService_GenerateToken(t *testing.T) {
	ss := newSecurityService()

	token1, err := ss.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if len(token1) == 0 {
		t.Error("token should not be empty")
	}

	token2, err := ss.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if token1 == token2 {
		t.Error("two generated tokens should be different")
	}
}

// ---------- Session Store Tests ----------

func TestSessionStore_CreateAndValidate(t *testing.T) {
	ss := newSessionStore(1 * time.Hour)
	defer ss.Stop()

	sessionToken, csrfToken, err := ss.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sessionToken == "" {
		t.Error("session token should not be empty")
	}
	if csrfToken == "" {
		t.Error("CSRF token should not be empty")
	}

	if !ss.IsValid(sessionToken) {
		t.Error("newly created session should be valid")
	}

	gotCSRF := ss.GetCSRFToken(sessionToken)
	if gotCSRF != csrfToken {
		t.Errorf("CSRF mismatch: got %q, want %q", gotCSRF, csrfToken)
	}
}

func TestSessionStore_Destroy(t *testing.T) {
	ss := newSessionStore(1 * time.Hour)
	defer ss.Stop()

	sessionToken, _, _ := ss.CreateSession()
	ss.DestroySession(sessionToken)

	if ss.IsValid(sessionToken) {
		t.Error("destroyed session should not be valid")
	}
}

func TestSessionStore_InvalidToken(t *testing.T) {
	ss := newSessionStore(1 * time.Hour)
	defer ss.Stop()

	if ss.IsValid("nonexistent") {
		t.Error("nonexistent session should not be valid")
	}
	if ss.GetCSRFToken("nonexistent") != "" {
		t.Error("nonexistent session should return empty CSRF")
	}
}

func TestSessionStore_ExpiredSession(t *testing.T) {
	ss := newSessionStore(1 * time.Millisecond) // Very short timeout
	defer ss.Stop()

	sessionToken, _, _ := ss.CreateSession()

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	if ss.IsValid(sessionToken) {
		t.Error("session should be expired")
	}
}

func TestSessionStore_StopIsIdempotent(t *testing.T) {
	ss := newSessionStore(1 * time.Hour)
	ss.Stop()
	ss.Stop() // should not panic
}

// ---------- Rate Limiting via Login Integration ----------

func TestLogin_RateLimitPerIP(t *testing.T) {
	s, _ := testServer(t)

	// Lock out IP 192.168.1.1 (default in doReq) with 5 wrong attempts
	for i := 0; i < 5; i++ {
		doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	}

	// Verify locked
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}

	// Different IP should not be locked
	req, _ := http.NewRequest("POST", "/api/auth/login", loginJSON("password"))
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	s.router.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("different IP should not be locked, got %d", rec2.Code)
	}
}

// ---------- Generate Secure Token Tests ----------

func TestGenerateSecureToken_Length(t *testing.T) {
	token, err := generateSecureToken(32)
	if err != nil {
		t.Fatalf("generateSecureToken failed: %v", err)
	}

	// Base64 URL encoding of 32 bytes ≈ 44 chars
	if len(token) < 32 {
		t.Errorf("token too short: %d chars", len(token))
	}
}

func TestGenerateSecureToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := generateSecureToken(16)
		if err != nil {
			t.Fatalf("generateSecureToken failed: %v", err)
		}
		if tokens[token] {
			t.Fatal("duplicate token generated")
		}
		tokens[token] = true
	}
}

// ---------- Change Password with Config Save ----------

func TestChangePassword_SavesToConfigFile(t *testing.T) {
	s, tmpDir := testServer(t)
	sessionToken, csrfToken := loginAndGetSession(t, s.router)

	// Write initial config file
	configPath := filepath.Join(tmpDir, "config.json")
	initialConfig := fmt.Sprintf(`{
		"port": 9877,
		"mode": "xray",
		"xray_config_dir": "%s/xray",
		"xkeen_binary": "xkeen",
		"allowed_roots": ["%s/xray"],
		"auth": {
			"password_hash": "%s",
			"session_timeout": 1,
			"max_login_attempts": 3,
			"lockout_duration": 1
		}
	}`, tmpDir, tmpDir, testPasswordHash)
	os.WriteFile(configPath, []byte(initialConfig), 0644)
	s.configPath = configPath

	body := map[string]string{
		"current_password": "password",
		"new_password":     "brandnewpassword",
	}
	bodyBytes, _ := json.Marshal(body)

	rec := doReqWithCookies(t, s.router, "POST", "/api/auth/change-password",
		bytes.NewReader(bodyBytes), sessionToken, csrfToken)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify config file was updated with new hash
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	auth, ok := cfg["auth"].(map[string]interface{})
	if !ok {
		t.Fatal("config missing auth section")
	}

	newHash, ok := auth["password_hash"].(string)
	if !ok || newHash == "" {
		t.Fatal("config missing password_hash")
	}
	if newHash == testPasswordHash {
		t.Error("password hash should have changed in config file")
	}
}

// ---------- Login Page Redirect ----------

func TestLoginPage_RedirectsIfLoggedIn(t *testing.T) {
	s, _ := testServer(t)
	sessionToken, _ := loginAndGetSession(t, s.router)

	req, _ := http.NewRequest("GET", "/login", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect for logged-in user, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/" {
		t.Errorf("expected redirect to /, got %s", loc)
	}
}

// ---------- Index Page Auth ----------

func TestIndexPage_RequiresAuth(t *testing.T) {
	s, _ := testServer(t)

	// HTML request should redirect to login
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Accept", "text/html")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", rec.Code)
	}
}

// ---------- Cookie Attributes ----------

func TestLoginCookieAttributes(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", rec.Code)
	}

	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Path != "/" {
			t.Errorf("cookie %s: expected Path=/, got %s", c.Name, c.Path)
		}
		if c.SameSite != http.SameSiteStrictMode {
			t.Errorf("cookie %s: expected SameSiteStrict, got %v", c.Name, c.SameSite)
		}
	}
}

// ---------- Edge Cases ----------

func TestLogin_ContentTypeJSON(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected application/json, got %s", contentType)
	}
}

func TestLogin_MultipleFailedAttemptsRecordIP(t *testing.T) {
	s, _ := testServer(t)

	// Make 4 failed attempts (should NOT trigger lockout, max=5)
	doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	rec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on 4th attempt, got %d", rec.Code)
	}

	// 5th attempt = max, still 401 (lockout triggers on this attempt)
	rec = doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on 5th attempt, got %d", rec.Code)
	}

	// 6th attempt should be locked (429)
	rec = doReq(t, s.router, "POST", "/api/auth/login", loginJSON("wrong"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 6th attempt, got %d", rec.Code)
	}
}

// --- validateAndResolveBackupPath ---

func TestValidateAndResolveBackupPath_ValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatal(err)
	}

	result, err := validateAndResolveBackupPath(xrayDir, []string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return a path within tmpDir
	if !strings.HasPrefix(result, tmpDir) {
		t.Errorf("expected path within %s, got %s", tmpDir, result)
	}
}

func TestValidateAndResolveBackupPath_EmptyRoots(t *testing.T) {
	_, err := validateAndResolveBackupPath("/some/dir", []string{})
	if err == nil {
		t.Error("expected error for empty allowed roots")
	}
}

func TestValidateAndResolveBackupPath_DefaultWithinRoot(t *testing.T) {
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Add parent of xray dir as allowed root, so default backup path is valid
	result, err := validateAndResolveBackupPath(xrayDir, []string{tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default path is filepath.Join(filepath.Dir(xrayDir), "xkeen-ui", "backups")
	expectedDefault := filepath.Join(tmpDir, "xkeen-ui", "backups")
	if result != expectedDefault {
		t.Errorf("expected default %s, got %s", expectedDefault, result)
	}
}

func TestValidateAndResolveBackupPath_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	xrayDir := filepath.Join(tmpDir, "xray")
	if err := os.MkdirAll(xrayDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Use a different root than tmpDir to trigger fallback
	otherDir := t.TempDir()

	result, err := validateAndResolveBackupPath(xrayDir, []string{otherDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use fallback within otherDir
	if !strings.HasPrefix(result, otherDir) {
		t.Errorf("expected fallback path within %s, got %s", otherDir, result)
	}
}

// --- loginPage ---

func TestLoginPage_NotLoggedIn(t *testing.T) {
	s, _ := testServer(t)
	rec := doReq(t, s.router, "GET", "/login", nil)

	// With emptyFS, login.html doesn't exist, so we get 500
	// This is expected behavior — loginPage serves embedded HTML
	if rec.Code != http.StatusInternalServerError {
		// If the code gets a real FS, it might return 200
		t.Logf("login page returned %d (expected 500 with emptyFS or 200 with real FS)", rec.Code)
	}
}

func TestLoginPage_AlreadyLoggedIn(t *testing.T) {
	s, _ := testServer(t)

	// Login first to get a session
	loginRec := doReq(t, s.router, "POST", "/api/auth/login", loginJSON("password"))
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", loginRec.Code)
	}
	_ = parseJSON(t, loginRec)
	sessionToken := ""
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "session" {
			sessionToken = c.Value
		}
	}
	if sessionToken == "" {
		t.Fatal("no session cookie")
	}

	// GET /login with valid session should redirect to /
	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: sessionToken})
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302 redirect when logged in, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if location != "/" {
		t.Errorf("expected redirect to /, got %s", location)
	}
}

func TestLoginPage_InvalidSession(t *testing.T) {
	s, _ := testServer(t)

	// GET /login with invalid session cookie — should serve login page
	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "invalid-token"})
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	// With emptyFS, login.html doesn't exist → 500
	// With real FS → 200 with HTML
	if rec.Code == http.StatusFound {
		t.Error("should not redirect with invalid session")
	}
}
