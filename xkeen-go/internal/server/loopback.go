package server

import (
	"net"
	"net/http"
)

// loopbackOnly wraps a handler so it only serves requests whose client
// address is loopback. It exists for endpoints that must be callable by
// local system hooks (e.g. the NDM netfilter.d script that restores AWG
// firewall rules after every NDMS rebuild) which cannot carry a session
// cookie or CSRF token. Anything arriving over the network is a 403 — the
// UI itself uses the authenticated route, not this wrapper.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden: loopback only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
