package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
)

// AuthTestHelper simplifies authentication testing by providing
// convenient methods for login, authenticated requests, and session management.
type AuthTestHelper struct {
	Server       *httptest.Server
	Client       *http.Client
	Cookies      []*http.Cookie
	CSRFToken    string
	baseURL      string
	lastResponse *http.Response
}

// NewAuthTestHelper creates a new authentication test helper
// wrapping the provided HTTP handler.
func NewAuthTestHelper(handler http.Handler) *AuthTestHelper {
	server := httptest.NewServer(handler)

	return &AuthTestHelper{
		Server:    server,
		Client:    server.Client(),
		Cookies:   make([]*http.Cookie, 0),
		baseURL:   server.URL,
		CSRFToken: "",
	}
}

// Login performs a login request with password only.
// Stores session cookies for subsequent authenticated requests.
func (h *AuthTestHelper) Login(password string) (*http.Response, error) {
	body := map[string]string{
		"password": password,
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := h.Client.Post(h.baseURL+"/api/auth/login", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	// Store cookies for subsequent requests
	h.Cookies = resp.Cookies()
	h.lastResponse = resp

	// Try to extract CSRF token from response
	h.extractCSRFToken(resp)

	return resp, nil
}

// LoginForm performs a login request using form data.
func (h *AuthTestHelper) LoginForm(password string) (*http.Response, error) {
	formData := url.Values{}
	formData.Set("password", password)

	resp, err := h.Client.PostForm(h.baseURL+"/api/auth/login", formData)
	if err != nil {
		return nil, err
	}

	h.Cookies = resp.Cookies()
	h.lastResponse = resp
	h.extractCSRFToken(resp)

	return resp, nil
}

// Setup performs initial setup with password.
func (h *AuthTestHelper) Setup(password string) (*http.Response, error) {
	body := map[string]string{
		"password": password,
	}
	bodyBytes, _ := json.Marshal(body)

	resp, err := h.Client.Post(h.baseURL+"/api/auth/setup", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	h.Cookies = resp.Cookies()
	h.lastResponse = resp
	h.extractCSRFToken(resp)

	return resp, nil
}

// Logout performs a logout request.
func (h *AuthTestHelper) Logout() (*http.Response, error) {
	req, err := h.newAuthenticatedRequest("POST", "/api/auth/logout", nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}

	// Clear stored cookies on successful logout
	if resp.StatusCode == http.StatusOK {
		h.Cookies = nil
		h.CSRFToken = ""
	}

	h.lastResponse = resp
	return resp, nil
}

// AuthenticatedRequest makes an authenticated request with the stored session cookie.
func (h *AuthTestHelper) AuthenticatedRequest(method, path string) (*http.Response, error) {
	req, err := h.newAuthenticatedRequest(method, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}

	h.lastResponse = resp
	return resp, nil
}

// AuthenticatedRequestWithBody makes an authenticated request with a body.
func (h *AuthTestHelper) AuthenticatedRequestWithBody(method, path string, body io.Reader) (*http.Response, error) {
	req, err := h.newAuthenticatedRequest(method, path, body)
	if err != nil {
		return nil, err
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}

	h.lastResponse = resp
	return resp, nil
}

// AuthenticatedRequestJSON makes an authenticated request with a JSON body.
func (h *AuthTestHelper) AuthenticatedRequestJSON(method, path string, data interface{}) (*http.Response, error) {
	bodyBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	req, err := h.newAuthenticatedRequest(method, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}

	h.lastResponse = resp
	return resp, nil
}

// Get makes an authenticated GET request.
func (h *AuthTestHelper) Get(path string) (*http.Response, error) {
	return h.AuthenticatedRequest(http.MethodGet, path)
}

// Post makes an authenticated POST request with a body.
func (h *AuthTestHelper) Post(path string, body io.Reader) (*http.Response, error) {
	return h.AuthenticatedRequestWithBody(http.MethodPost, path, body)
}

// PostJSON makes an authenticated POST request with a JSON body.
func (h *AuthTestHelper) PostJSON(path string, data interface{}) (*http.Response, error) {
	return h.AuthenticatedRequestJSON(http.MethodPost, path, data)
}

// Put makes an authenticated PUT request with a body.
func (h *AuthTestHelper) Put(path string, body io.Reader) (*http.Response, error) {
	return h.AuthenticatedRequestWithBody(http.MethodPut, path, body)
}

// PutJSON makes an authenticated PUT request with a JSON body.
func (h *AuthTestHelper) PutJSON(path string, data interface{}) (*http.Response, error) {
	return h.AuthenticatedRequestJSON(http.MethodPut, path, data)
}

// Delete makes an authenticated DELETE request.
func (h *AuthTestHelper) Delete(path string) (*http.Response, error) {
	return h.AuthenticatedRequest(http.MethodDelete, path)
}

// UnauthenticatedRequest makes a request without authentication.
func (h *AuthTestHelper) UnauthenticatedRequest(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, h.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}

	h.lastResponse = resp
	return resp, nil
}

// RequestWithCookies makes a request with specific cookies.
func (h *AuthTestHelper) RequestWithCookies(method, path string, cookies []*http.Cookie) (*http.Response, error) {
	req, err := http.NewRequest(method, h.baseURL+path, nil)
	if err != nil {
		return nil, err
	}

	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}

	h.lastResponse = resp
	return resp, nil
}

// SetCSRFToken manually sets the CSRF token.
func (h *AuthTestHelper) SetCSRFToken(token string) {
	h.CSRFToken = token
}

// SetCookies manually sets cookies for authenticated requests.
func (h *AuthTestHelper) SetCookies(cookies []*http.Cookie) {
	h.Cookies = cookies
}

// ClearAuth clears stored authentication data.
func (h *AuthTestHelper) ClearAuth() {
	h.Cookies = nil
	h.CSRFToken = ""
}

// GetLastResponse returns the last response received.
func (h *AuthTestHelper) GetLastResponse() *http.Response {
	return h.lastResponse
}

// GetURL returns the full URL for a given path.
func (h *AuthTestHelper) GetURL(path string) string {
	return h.baseURL + path
}

// Close closes the test server.
func (h *AuthTestHelper) Close() {
	h.Server.Close()
}

// IsAuthenticated checks if there are stored cookies.
func (h *AuthTestHelper) IsAuthenticated() bool {
	return len(h.Cookies) > 0
}

// ParseJSONResponse parses a JSON response into the provided struct.
func (h *AuthTestHelper) ParseJSONResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	return json.Unmarshal(body, v)
}

// ReadResponseBody reads and returns the response body as a string.
func (h *AuthTestHelper) ReadResponseBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}
	return string(body), nil
}

// AssertStatusCode is a helper that returns an error if the status code doesn't match.
func (h *AuthTestHelper) AssertStatusCode(resp *http.Response, expected int) error {
	if resp.StatusCode != expected {
		body, _ := h.ReadResponseBody(resp)
		return fmt.Errorf("expected status %d, got %d. Body: %s", expected, resp.StatusCode, body)
	}
	return nil
}

// newAuthenticatedRequest creates a new request with authentication headers.
func (h *AuthTestHelper) newAuthenticatedRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, h.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	// Add stored cookies
	for _, c := range h.Cookies {
		req.AddCookie(c)
	}

	// Add CSRF token if available
	if h.CSRFToken != "" {
		req.Header.Set("X-CSRF-Token", h.CSRFToken)
	}

	return req, nil
}

// extractCSRFToken attempts to extract CSRF token from response.
func (h *AuthTestHelper) extractCSRFToken(resp *http.Response) {
	// Try to get from header first
	if token := resp.Header.Get("X-CSRF-Token"); token != "" {
		h.CSRFToken = token
		return
	}

	// Try to parse from JSON body
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	// Restore body for later reading
	resp.Body = io.NopCloser(bytes.NewReader(body))

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return
	}

	if token, ok := data["csrf_token"].(string); ok {
		h.CSRFToken = token
	} else if token, ok := data["csrfToken"].(string); ok {
		h.CSRFToken = token
	}
}

// URL returns the base URL of the test server.
func (h *AuthTestHelper) URL() string {
	return h.baseURL
}

// Session represents an authenticated session for testing.
type Session struct {
	Helper    *AuthTestHelper
	Cookies   []*http.Cookie
	CSRFToken string
}

// NewSession creates a new authenticated session.
func (h *AuthTestHelper) NewSession(password string) (*Session, error) {
	resp, err := h.Login(password)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("login failed with status %d: %s", resp.StatusCode, string(body))
	}

	return &Session{
		Helper:    h,
		Cookies:   h.Cookies,
		CSRFToken: h.CSRFToken,
	}, nil
}

// Request makes an authenticated request within this session.
func (s *Session) Request(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, s.Helper.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	for _, c := range s.Cookies {
		req.AddCookie(c)
	}

	if s.CSRFToken != "" {
		req.Header.Set("X-CSRF-Token", s.CSRFToken)
	}

	return s.Helper.Client.Do(req)
}

// Cleanup closes the test server and releases resources.
// This is an alias for Close() for convenience.
func (h *AuthTestHelper) Cleanup() {
	h.Close()
}

// Status checks the authentication status.
func (h *AuthTestHelper) Status() (*http.Response, error) {
	return h.AuthenticatedRequest(http.MethodGet, "/api/auth/status")
}

// ChangePassword attempts to change the password for the current session.
func (h *AuthTestHelper) ChangePassword(currentPassword, newPassword string) (*http.Response, error) {
	body := map[string]string{
		"current_password": currentPassword,
		"new_password":     newPassword,
	}
	return h.AuthenticatedRequestJSON(http.MethodPost, "/api/auth/change-password", body)
}

// Do performs a raw HTTP request with the test client.
func (h *AuthTestHelper) Do(req *http.Request) (*http.Response, error) {
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}
	h.lastResponse = resp
	return resp, nil
}

// NewRequest creates a new HTTP request.
func (h *AuthTestHelper) NewRequest(method, path string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(method, h.baseURL+path, body)
}

// CheckHealth performs a health check on the server.
func (h *AuthTestHelper) CheckHealth() (*http.Response, error) {
	return h.UnauthenticatedRequest(http.MethodGet, "/api/health", nil)
}

// WaitForServer waits for the server to be ready by polling the health endpoint.
// Returns an error if the server doesn't respond within the specified number of attempts.
func (h *AuthTestHelper) WaitForServer(maxAttempts int) error {
	for i := 0; i < maxAttempts; i++ {
		resp, err := h.CheckHealth()
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	return fmt.Errorf("server not ready after %d attempts", maxAttempts)
}
