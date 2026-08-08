package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestPersistentSessionStore_Reload(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	ss := newPersistentSessionStore(1*time.Hour, filePath)
	token, csrf, err := ss.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	token2, csrf2, err := ss.CreateSession()
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	ss.Stop()

	// Simulate a process restart: a new store over the same file must see
	// both sessions with their CSRF tokens intact.
	ss2 := newPersistentSessionStore(1*time.Hour, filePath)
	defer ss2.Stop()

	if !ss2.IsValid(token) {
		t.Error("session 1 should survive a restart")
	}
	if !ss2.IsValid(token2) {
		t.Error("session 2 should survive a restart")
	}
	if got := ss2.GetCSRFToken(token); got != csrf {
		t.Errorf("CSRF mismatch after reload: got %q, want %q", got, csrf)
	}
	if got := ss2.GetCSRFToken(token2); got != csrf2 {
		t.Errorf("CSRF mismatch after reload: got %q, want %q", got, csrf2)
	}
}

func TestPersistentSessionStore_DestroyPersists(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	ss := newPersistentSessionStore(1*time.Hour, filePath)
	token, _, _ := ss.CreateSession()
	ss.DestroySession(token)
	ss.Stop()

	// A destroyed session must not come back after a restart.
	ss2 := newPersistentSessionStore(1*time.Hour, filePath)
	defer ss2.Stop()
	if ss2.IsValid(token) {
		t.Error("destroyed session should not survive a restart")
	}
}

func TestPersistentSessionStore_ExpiredNotReloaded(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")

	ss := newPersistentSessionStore(1*time.Millisecond, filePath)
	token, _, _ := ss.CreateSession()
	ss.Stop()
	time.Sleep(20 * time.Millisecond) // Let the session expire on disk.

	ss2 := newPersistentSessionStore(1*time.Hour, filePath)
	defer ss2.Stop()
	if ss2.IsValid(token) {
		t.Error("expired session should not be restored")
	}
}

func TestPersistentSessionStore_CorruptFile(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(filePath, []byte("{ not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	ss := newPersistentSessionStore(1*time.Hour, filePath)
	defer ss.Stop()
	if ss.IsValid("anything") {
		t.Error("corrupt sessions file must yield an empty store")
	}
}

func TestPersistentSessionStore_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode assertions are meaningless on Windows")
	}
	filePath := filepath.Join(t.TempDir(), "sessions.json")
	ss := newPersistentSessionStore(1*time.Hour, filePath)
	_, _, _ = ss.CreateSession()
	ss.Stop()

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("sessions file must not be group/world accessible, got %o", perm)
	}
}

func TestSessionStore_MemoryOnlyCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	// newSessionStore keeps the memory-only behavior: no file should appear.
	ss := newSessionStore(1 * time.Hour)
	defer ss.Stop()
	_, _, _ = ss.CreateSession()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("memory-only store must not create files, found %d entries", len(entries))
	}
}

// newPersistentTestServer mirrors the minimal auth route setup from testServer
// but backs the session store with a persistent file (simulating a real
// deployment where sessions.json lives next to the config).
func newPersistentTestServer(t *testing.T, configPath, sessionsPath string) *Server {
	t.Helper()
	cfg := testConfig(t, filepath.Dir(configPath))
	s := &Server{
		cfg:        cfg,
		configPath: configPath,
		router:     mux.NewRouter(),
		webFS:      &emptyFS{},
	}
	sessionTimeout := time.Duration(cfg.Auth.SessionTimeout) * time.Hour
	s.sessions = newPersistentSessionStore(sessionTimeout, sessionsPath)
	s.security = newSecurityService()
	s.middleware = NewMiddleware(s.sessions, s.security)

	s.router.HandleFunc("/login", s.loginPage).Methods("GET")
	s.router.Use(SecurityHeadersMiddleware)
	s.router.Use(BodySizeLimitMiddleware(MaxBodyBytes))
	s.router.Handle("/api/auth/login", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.login))).Methods("POST")
	s.router.Handle("/api/auth/logout", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.logout))).Methods("POST")
	s.router.Handle("/api/auth/status", s.middleware.RateLimitMiddleware(http.HandlerFunc(s.authStatus))).Methods("GET")
	s.router.Handle("/health", http.HandlerFunc(s.healthCheck)).Methods("GET")

	apiRouter := s.router.PathPrefix("/api").Subrouter()
	apiRouter.Use(s.middleware.AuthMiddleware)
	apiRouter.Use(s.middleware.CSRFMiddleware)
	apiRouter.HandleFunc("/auth/csrf", s.getCSRFToken).Methods("GET")
	apiRouter.HandleFunc("/auth/change-password", s.changePassword).Methods("POST")

	s.router.Handle("/", s.middleware.AuthMiddleware(http.HandlerFunc(s.indexPage))).Methods("GET")
	return s
}

// TestSession_SurvivesServerRestart proves the SSE-pipe fix end to end: after
// an xkeen-ui restart (new process, same config dir), a logged-in browser's
// session cookie must still authenticate — otherwise the status stream gets a
// 401 and the UI "pipe" drops until the user logs in again.
func TestSession_SurvivesServerRestart(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	sessionsPath := filepath.Join(tmpDir, "sessions.json")

	// First process instance: login and capture the session.
	s1 := newPersistentTestServer(t, configPath, sessionsPath)
	sessionToken, csrfToken := loginAndGetSession(t, s1.router)
	s1.sessions.Stop()
	s1.middleware.Stop()

	// Second process instance over the same files: the old session cookie
	// must still authenticate.
	s2 := newPersistentTestServer(t, configPath, sessionsPath)
	defer func() {
		s2.sessions.Stop()
		s2.middleware.Stop()
	}()

	rec := doReqWithCookies(t, s2.router, "GET", "/api/auth/csrf", nil, sessionToken, csrfToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("session must survive a restart: got %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}
