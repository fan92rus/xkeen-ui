package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// sessionStore is the session manager with periodic cleanup.
//
// When filePath is non-empty the store persists sessions to disk after every
// change and reloads them on startup, so browser sessions survive process
// restarts. Without persistence, every xkeen-ui restart invalidates all
// sessions: the browser's SSE status stream gets a 401, the UI "pipe" drops,
// and the user must log in again.
type sessionStore struct {
	mu             sync.RWMutex
	sessions       map[string]*session
	sessionTimeout time.Duration
	cleanupTime    time.Duration
	stopCh         chan struct{}  // Channel for graceful shutdown
	stopped        bool           // Flag to prevent double stop
	wg             sync.WaitGroup // WaitGroup for goroutine completion
	filePath       string         // Optional persistence file; "" = memory-only
}

// session holds per-session state. Fields are exported so the map can be
// serialized to JSON for persistence.
type session struct {
	CSRFToken string    `json:"csrf_token"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// sessionsFile is the on-disk format for persisted sessions.
type sessionsFile struct {
	Sessions map[string]*session `json:"sessions"`
}

// newSessionStore creates an in-memory-only session store (no persistence).
func newSessionStore(sessionTimeout time.Duration) *sessionStore {
	return newPersistentSessionStore(sessionTimeout, "")
}

// newPersistentSessionStore creates a session store that persists sessions to
// filePath so they survive restarts. A missing or corrupt file is tolerated:
// the store starts empty and logs a warning.
func newPersistentSessionStore(sessionTimeout time.Duration, filePath string) *sessionStore {
	ss := &sessionStore{
		sessions:       make(map[string]*session),
		sessionTimeout: sessionTimeout,
		cleanupTime:    10 * time.Minute,
		stopCh:         make(chan struct{}),
		stopped:        false,
		filePath:       filePath,
	}

	// Load persisted sessions before starting the cleanup goroutine so the
	// store is fully initialized once concurrent requests can reach it.
	if filePath != "" {
		if err := ss.load(); err != nil {
			log.Printf("Warning: failed to load sessions from %s: %v", filePath, err)
		}
	}

	// Start cleanup goroutine
	ss.wg.Add(1)
	go ss.cleanupLoop()

	return ss
}

// load reads persisted sessions from disk into the store, dropping expired
// entries. Missing or empty files are not an error.
func (ss *sessionStore) load() error {
	data, err := os.ReadFile(ss.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // First run — nothing persisted yet.
		}
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	var file sessionsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("invalid sessions file: %w", err)
	}

	now := time.Now()
	for token, sess := range file.Sessions {
		if sess == nil || now.After(sess.ExpiresAt) {
			continue // Skip nil and already-expired entries.
		}
		ss.sessions[token] = sess
	}
	return nil
}

// persist atomically writes the current session map to disk. No-op when the
// store is memory-only. Callers must hold ss.mu (write lock).
func (ss *sessionStore) persist() {
	if ss.filePath == "" {
		return
	}

	data, err := json.Marshal(sessionsFile{Sessions: ss.sessions})
	if err != nil {
		log.Printf("Warning: failed to serialize sessions: %v", err)
		return
	}

	// Write to a temp file in the same directory, then rename into place so a
	// crash mid-write can never leave a truncated sessions file behind.
	if dir := filepath.Dir(ss.filePath); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			log.Printf("Warning: failed to create session directory %s: %v", dir, err)
			return
		}
	}

	tmpPath := ss.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		log.Printf("Warning: failed to write sessions file %s: %v", tmpPath, err)
		return
	}
	if err := os.Rename(tmpPath, ss.filePath); err != nil {
		log.Printf("Warning: failed to persist sessions to %s: %v", ss.filePath, err)
		return
	}
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
	changed := false
	for token, sess := range ss.sessions {
		// Remove expired sessions
		if now.After(sess.ExpiresAt) {
			delete(ss.sessions, token)
			changed = true
		}
	}

	// Only touch disk if something was actually removed.
	if changed {
		ss.persist()
	}
}

func (ss *sessionStore) IsValid(sessionToken string) bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	sess, exists := ss.sessions[sessionToken]
	if !exists {
		return false
	}

	if time.Now().After(sess.ExpiresAt) {
		return false
	}

	return true
}

func (ss *sessionStore) GetCSRFToken(sessionToken string) string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	sess, exists := ss.sessions[sessionToken]
	if !exists {
		return ""
	}

	return sess.CSRFToken
}

func (ss *sessionStore) CreateSession() (sessionToken, csrfToken string, err error) {
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
		CSRFToken: csrfToken,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ss.sessionTimeout),
	}

	// Persist so the session survives a restart (atomic tmp+rename).
	ss.persist()

	return sessionToken, csrfToken, nil
}

func (ss *sessionStore) DestroySession(sessionToken string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if _, exists := ss.sessions[sessionToken]; !exists {
		return
	}
	delete(ss.sessions, sessionToken)
	ss.persist()
}
