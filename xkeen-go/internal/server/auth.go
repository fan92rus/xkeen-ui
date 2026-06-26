package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
)

// API response types for auth handlers.
// Using typed structs instead of map[string]interface{} for compile-time safety.

type loginResponse struct {
	OK                    bool   `json:"ok"`
	CSRFToken             string `json:"csrf_token,omitempty"`
	RequirePasswordChange bool   `json:"require_password_change,omitempty"`
	Message               string `json:"message,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type statusResponse struct {
	OK       bool `json:"ok"`
	LoggedIn bool `json:"logged_in"`
}

type messageResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// writeJSON is a helper that serializes v as JSON to w and sets Content-Type.
func writeJSON(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON error: %v", err)
	}
}

// Auth handlers (login page, login/logout, password change, index page).

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	// Check if already logged in
	if cookie, err := r.Cookie("session"); err == nil {
		if s.sessions.IsValid(cookie.Value) {
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
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Check password from config
	storedHash := s.cfg.Auth.PasswordHash
	if storedHash == "" {
		// If no hash set, use default admin/admin (cached to avoid bcrypt on every login)
		if s.defaultHash == "" {
			var err error
			s.defaultHash, err = s.security.HashPassword("admin")
			if err != nil {
				http.Error(w, "Internal error", http.StatusInternalServerError)
				return
			}
		}
		storedHash = s.defaultHash
	}

	if !s.security.CheckPassword(req.Password, storedHash) {
		// Record failed attempt
		s.middleware.RecordFailedAttempt(r.Context())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, http.StatusUnauthorized, &loginResponse{OK: false, Error: "Invalid password"})
		return
	}

	// Reset rate limit on successful login
	s.middleware.ResetAttempts(r.Context())

	// Create session
	sessionToken, csrfToken, err := s.sessions.CreateSession()
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	s.SetSessionCookie(w, sessionToken)

	// Set CSRF token cookie (for client-side access)
	s.SetCSRFTokenCookie(w, csrfToken)

	// Build response with optional password change requirement
	resp := &loginResponse{OK: true, CSRFToken: csrfToken}
	if s.cfg.Auth.ForcePasswordChange {
		resp.RequirePasswordChange = true
		resp.Message = "You must change the default password before continuing"
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		s.sessions.DestroySession(cookie.Value)
	}
	ClearSessionCookie(w, s.cfg.CookieSecure)

	writeJSON(w, http.StatusOK, &messageResponse{OK: true})
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	loggedIn := false

	if cookie, err := r.Cookie("session"); err == nil {
		if s.sessions.IsValid(cookie.Value) {
			loggedIn = true
		}
	}

	writeJSON(w, http.StatusOK, &statusResponse{OK: true, LoggedIn: loggedIn})
}

// changePassword handles password change requests.
// POST /api/auth/change-password
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &messageResponse{OK: false, Error: "Invalid request body"})
		return
	}

	// Validate input
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, &messageResponse{OK: false, Error: "Current password and new password are required"})
		return
	}

	// Validate new password length
	if len(req.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, &messageResponse{OK: false, Error: "New password must be at least 8 characters long"})
		return
	}

	// Check if new password is same as current
	if req.CurrentPassword == req.NewPassword {
		writeJSON(w, http.StatusBadRequest, &messageResponse{OK: false, Error: "New password must be different from current password"})
		return
	}

	// Verify current password
	storedHash := s.cfg.Auth.PasswordHash
	if storedHash == "" {
		if s.defaultHash == "" {
			var err error
			s.defaultHash, err = s.security.HashPassword("admin")
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, &messageResponse{OK: false, Error: "Internal server error"})
				return
			}
		}
		storedHash = s.defaultHash
	}

	if !s.security.CheckPassword(req.CurrentPassword, storedHash) {
		writeJSON(w, http.StatusUnauthorized, &messageResponse{OK: false, Error: "Current password is incorrect"})
		return
	}

	// Hash new password
	newHash, err := s.security.HashPassword(req.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, &messageResponse{OK: false, Error: "Failed to hash new password"})
		return
	}

	// Update config
	s.cfg.Auth.PasswordHash = newHash
	s.cfg.Auth.ForcePasswordChange = false // Clear force password change flag

	// Save config to file
	if err := s.cfg.SaveConfig(s.configPath); err != nil {
		log.Printf("Failed to save config: %v", err)
		writeJSON(w, http.StatusInternalServerError, &messageResponse{OK: false, Error: "Failed to save configuration"})
		return
	}

	log.Printf("Password changed successfully")

	writeJSON(w, http.StatusOK, &messageResponse{OK: true, Message: "Password changed successfully"})
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
