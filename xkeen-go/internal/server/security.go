package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// securityService implements SecurityService interface.
type securityService struct {
	bcryptCost int
}

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
// The Secure flag is controlled by cfg.CookieSecure (enabled when served over HTTPS).
func (s *Server) SetSessionCookie(w http.ResponseWriter, token string) {
	maxAge := s.cfg.Auth.SessionTimeout * 3600 // Convert hours to seconds
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
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
		Secure:   s.cfg.CookieSecure,
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
