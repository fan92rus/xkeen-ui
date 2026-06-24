package server

import (
	"fmt"
	"sync"
	"time"
)

// sessionStore is the in-memory session manager with periodic cleanup.
type sessionStore struct {
	mu             sync.RWMutex
	sessions       map[string]*session
	sessionTimeout time.Duration
	cleanupTime    time.Duration
	stopCh         chan struct{}  // Channel for graceful shutdown
	stopped        bool           // Flag to prevent double stop
	wg             sync.WaitGroup // WaitGroup for goroutine completion
}

type session struct {
	csrfToken string
	createdAt time.Time
	expiresAt time.Time
}

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

func (ss *sessionStore) IsValid(sessionToken string) bool {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	sess, exists := ss.sessions[sessionToken]
	if !exists {
		return false
	}

	if time.Now().After(sess.expiresAt) {
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

	return sess.csrfToken
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
