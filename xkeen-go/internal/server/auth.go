package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
)

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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "Invalid password",
		})
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

	// Check if password change is required (default credentials)
	response := map[string]interface{}{
		"ok":         true,
		"csrf_token": csrfToken,
	}

	if s.cfg.Auth.ForcePasswordChange {
		response["require_password_change"] = true
		response["message"] = "You must change the default password before continuing"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

	if cookie, err := r.Cookie("session"); err == nil {
		if s.sessions.IsValid(cookie.Value) {
			loggedIn = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":        true,
		"logged_in": loggedIn,
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
		if s.defaultHash == "" {
			var err error
			s.defaultHash, err = s.security.HashPassword("admin")
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
		storedHash = s.defaultHash
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
	s.cfg.Auth.ForcePasswordChange = false // Clear force password change flag

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

	log.Printf("Password changed successfully")

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
