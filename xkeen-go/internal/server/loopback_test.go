package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// loopbackOnly must allow unauthenticated POSTs from localhost (the NDM
// netfilter.d hook restores AWG firewall rules after every NDMS rebuild and
// has no session or CSRF token) and reject everything else.
func TestLoopbackOnly(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := loopbackOnly(ok)

	cases := []struct {
		remote string
		want   int
	}{
		{"127.0.0.1:12345", http.StatusOK},
		{"[::1]:12345", http.StatusOK},
		{"192.168.1.5:4444", http.StatusForbidden},
		{"10.8.0.4:55555", http.StatusForbidden},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/awg/restore-firewall", http.NoBody)
		req.RemoteAddr = c.remote
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("RemoteAddr %q: got %d, want %d", c.remote, rec.Code, c.want)
		}
	}
}

func TestLoopbackOnly_RejectsEmptyRemoteAddr(t *testing.T) {
	h := loopbackOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	req.RemoteAddr = ""
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "loopback") {
		t.Fatalf("empty RemoteAddr must be rejected, got %d %q", rec.Code, rec.Body.String())
	}
}
