package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/fan92rus/xkeen-ui/internal/happ"
)

// Source constants describe how the last successful fetch was delivered.
const (
	// SourceProxy — fetch succeeded through the SOCKS5/HTTP inbound of a
	// running Xray instance (traffic went through the VPN tunnel).
	SourceProxy = "xray-proxy"
	// SourceDirect — fetch succeeded via a direct connection from the host
	// (no tunnel; used as a fallback or when no inbound proxy is configured).
	SourceDirect = "direct"
)

// perAttemptTimeout is the budget for a single cascade attempt (proxy or
// direct). Two attempts fit within the historical 30s client timeout while
// still leaving headroom.
const perAttemptTimeout = 20 * time.Second

// Fetcher downloads and parses subscription content from a URL.
type Fetcher struct {
	client *http.Client

	// proxyURL, if non-empty, routes fetches through a local SOCKS5/HTTP
	// inbound (typically Xray on 127.0.0.1). When empty, fetches go direct.
	proxyURL string
	proxyMu  sync.RWMutex

	// HAPPHWID is the hardware ID sent as X-HWID header when fetching
	// HAPP-encrypted subscriptions (happ://crypt5/ links). If empty,
	// the server returns placeholder data.
	HAPPHWID string
}

// NewFetcher creates a Fetcher with a 30-second timeout, Hiddify User-Agent,
// and a custom DNS resolver that falls back to public DNS if the system
// resolver is unavailable (common on Keenetic routers where local DNS
// may be intercepted by xray).
func NewFetcher() *Fetcher {
	transport := newDirectTransport()

	return &Fetcher{
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// NewFetcherWithClient creates a Fetcher with a custom HTTP client (for testing).
func NewFetcherWithClient(client *http.Client) *Fetcher {
	return &Fetcher{client: client}
}

// newDirectTransport builds an http.Transport that dials directly (no proxy)
// with a DNS resolver that falls back to public DNS if the system resolver
// is unavailable (common on Keenetic routers where local DNS may be
// intercepted by xray).
func newDirectTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, address string) (net.Conn, error) {
				return dialDNSServers(ctx, address)
			},
		},
	}

	return &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
}

// newProxiedTransport builds an http.Transport that routes all dialing
// through the given proxy URL. Supports socks5 (remote DNS resolution via
// the SOCKS5 server, so DNS leaks are prevented) and http/https schemes.
func newProxiedTransport(proxyURL string) (*http.Transport, error) {
	pu, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL %q: %w", proxyURL, err)
	}

	switch pu.Scheme {
	case "socks5", "socks5h":
		// proxy.SOCKS5 performs DNS resolution on the SOCKS5 server side
		// (remote DNS) when the address is a hostname. This prevents DNS
		// leaks through the host's resolver.
		hostPort := pu.Host
		if hostPort == "" {
			return nil, fmt.Errorf("socks5 proxy URL missing host:port")
		}
		var auth *proxy.Auth
		if pu.User != nil {
			pwd, _ := pu.User.Password()
			auth = &proxy.Auth{User: pu.User.Username(), Password: pwd}
		}
		socksDialer, err := proxy.SOCKS5("tcp", hostPort, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}
		return &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				// proxy.Dialer.Dial does not accept a context, so we wrap it
				// in a goroutine + select to honor cancellation/timeouts.
				// On ctx cancellation we abort the caller, but the background
				// dial goroutine continues until the kernel connect() returns
				// (bounded by the client Timeout). The returned conn, if any,
				// is closed immediately to avoid leaking a live connection.
				type result struct {
					conn net.Conn
					err  error
				}
				ch := make(chan result, 1)
				go func() {
					c, e := socksDialer.Dial(network, addr)
					ch <- result{c, e}
				}()
				select {
				case <-ctx.Done():
					// Best-effort cleanup if the dial completes late.
					go func() {
						if r := <-ch; r.conn != nil {
							_ = r.conn.Close()
						}
					}()
					return nil, ctx.Err()
				case r := <-ch:
					return r.conn, r.err
				}
			},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		}, nil

	case "http", "https":
		return &http.Transport{
			Proxy:                 http.ProxyURL(pu),
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (want socks5 or http)", pu.Scheme)
	}
}

// SetProxyURL configures the proxy used for subsequent fetches.
//
// Accepts "socks5://host:port", "socks5h://host:port", or
// "http://host:port". An empty string clears the proxy (fetches go direct).
//
// Returns an error for malformed URLs or unsupported schemes; the fetcher
// state is left unchanged in that case.
func (f *Fetcher) SetProxyURL(proxyURL string) error {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()

	if proxyURL == "" {
		f.proxyURL = ""
		return nil
	}

	// Validate up front so a bad URL is rejected before first use.
	pu, err := url.Parse(proxyURL)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}
	if pu.Scheme != "socks5" && pu.Scheme != "socks5h" && pu.Scheme != "http" && pu.Scheme != "https" {
		return fmt.Errorf("unsupported proxy scheme %q (want socks5 or http)", pu.Scheme)
	}
	if pu.Host == "" {
		return fmt.Errorf("proxy URL missing host:port")
	}

	f.proxyURL = proxyURL
	return nil
}

// HTTPClient returns the fetcher's internal HTTP client.
// Used by diagnostics to make requests through the same transport/cascade.
func (f *Fetcher) HTTPClient() *http.Client {
	return f.client
}

// ProxyStatus returns a human-readable description of the current proxy
// configuration: the proxy URL if set, or "direct" if fetches go direct.
func (f *Fetcher) ProxyStatus() string {
	f.proxyMu.RLock()
	defer f.proxyMu.RUnlock()
	if f.proxyURL != "" {
		return f.proxyURL
	}
	return "direct"
}

// FetchResult holds the parsed subscription entries together with the
// delivery method that produced them.
type FetchResult struct {
	Entries []*ProxyEntry
	Source  string // SourceProxy or SourceDirect
}

// FetchWithCascade downloads the subscription with a two-stage strategy:
//
//  1. If a proxy is configured (see SetProxyURL), try fetching through it
//     with a 20s budget. On success, return SourceProxy.
//  2. On proxy failure (or if no proxy is set), fall back to a direct
//     fetch with a 20s budget. On success, return SourceDirect.
//  3. If both stages fail, return an aggregated error describing both
//     attempts so the caller can surface a useful diagnostic.
//
// The parent ctx bounds the total operation; each stage also receives its
// own child timeout so a hung proxy does not consume the entire budget.
// deadlineOf returns the time remaining until ctx's deadline, or zero
// if ctx has no deadline. Used to decide whether a cascade stage has
// enough budget left to run.
func deadlineRemaining(ctx context.Context) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		return time.Until(dl)
	}
	return 0 // no deadline → unbounded
}

// attemptCtx returns a child context bounded by both the parent deadline
// and perAttemptTimeout, so a single stage can never run longer than
// perAttemptTimeout even if the parent still has budget — and never runs
// at all if the parent has already expired.
func attemptCtx(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := perAttemptTimeout
	if rem := deadlineRemaining(parent); rem > 0 && rem < timeout {
		timeout = rem
	}
	return context.WithTimeout(parent, timeout)
}

// FetchWithCascade downloads the subscription with a two-stage strategy:
//
//	1. If a proxy is configured (see SetProxyURL), try fetching through it
//	   with a 20s budget. On success, return SourceProxy.
//	2. On proxy failure (or if no proxy is set), fall back to a direct
//	   fetch with a 20s budget. On success, return SourceDirect.
//	3. If both stages fail, return an aggregated error describing both
//	   attempts so the caller can surface a useful diagnostic.
//
// The parent ctx bounds the total operation; each stage also receives its
// own child timeout so a hung proxy does not consume the entire budget.
func (f *Fetcher) FetchWithCascade(ctx context.Context, subURL string) (*FetchResult, error) {
	if subURL == "" {
		return nil, fmt.Errorf("subscription URL is empty")
	}

	// HAPP-encrypted links need special handling: decrypt first,
	// then HTTP fetch with X-HWID header, then convert sing-box JSON.
	if strings.HasPrefix(subURL, "happ://") {
		return f.fetchHAPP(ctx, subURL)
	}

	f.proxyMu.RLock()
	pURL := f.proxyURL
	f.proxyMu.RUnlock()

	var proxyErr error
	if pURL != "" {
		transport, err := newProxiedTransport(pURL)
		if err != nil {
			// Treat a bad proxy config as a proxy-stage failure and
			// fall through to direct.
			proxyErr = err
		} else {
			client := &http.Client{
				Timeout:   perAttemptTimeout,
				Transport: transport,
			}
			pCtx, pCancel := attemptCtx(ctx)
			entries, err := f.doFetch(pCtx, client, subURL)
			pCancel()
			if err == nil {
				return &FetchResult{Entries: entries, Source: SourceProxy}, nil
			}
			proxyErr = err
		}
	}

	// Direct attempt — own child context so a proxy timeout doesn't starve it.
	dCtx, dCancel := attemptCtx(ctx)
	directEntries, directErr := f.doFetch(dCtx, f.client, subURL)
	dCancel()
	if directErr == nil {
		return &FetchResult{Entries: directEntries, Source: SourceDirect}, nil
	}

	// Both failed (or proxy failed + direct failed).
	if proxyErr != nil {
		return nil, fmt.Errorf("proxy: %v; direct: %v", proxyErr, directErr)
	}
	return nil, fmt.Errorf("direct: %v", directErr)
}

// doFetch performs a single HTTP GET using the provided client, decodes
// base64 if needed, and parses each share URI into a ProxyEntry.
func (f *Fetcher) doFetch(ctx context.Context, client *http.Client, subURL string) ([]*ProxyEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Hiddify")
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("subscription returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10 MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read subscription body: %w", err)
	}

	entries, err := ParseSubscriptionContent(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse subscription content: %w", err)
	}

	return entries, nil
}

// Fetch downloads the subscription from url, decodes base64 if needed,
// and parses each share URI into a ProxyEntry.
//
// This is a backward-compatible wrapper around FetchWithCascade: it returns
// only the entries (the caller loses the source information). New callers
// should use FetchWithCascade.
func (f *Fetcher) Fetch(ctx context.Context, subURL string) ([]*ProxyEntry, error) {
	result, err := f.FetchWithCascade(ctx, subURL)
	if err != nil {
		return nil, err
	}
	return result.Entries, nil
}

// fetchHAPP handles happ://crypt5/ links: decrypts the link, fetches
// the real subscription URL with X-HWID header, and converts the
// sing-box JSON response to xkeen ProxyEntry values.
func (f *Fetcher) fetchHAPP(ctx context.Context, subURL string) (*FetchResult, error) {
	d, err := happ.NewDecryptorEmbedded()
	if err != nil {
		return nil, fmt.Errorf("happ: initializing decryptor: %w", err)
	}

	realURL, err := d.Decrypt(subURL)
	if err != nil {
		return nil, fmt.Errorf("happ: decrypting link: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("happ: creating request: %w", err)
	}

	req.Header.Set("User-Agent", "happ")
	if f.HAPPHWID != "" {
		req.Header.Set("X-HWID", f.HAPPHWID)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("happ: fetching subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("happ: server returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("happ: reading response: %w", err)
	}

	// Try sing-box JSON first (reference format). Each server has multiple
	// outbound protocols (vless + hysteria2), so JSON gives more entries.
	if entries, err := f.parseHAPPJSON(data); err == nil {
		return &FetchResult{Entries: entries, Source: SourceDirect}, nil
	}

	// Fall back to standard base64 URI format.
	entries, err := ParseSubscriptionContent(data)
	if err != nil {
		return nil, fmt.Errorf("happ: parsing subscription: %w", err)
	}

	return &FetchResult{Entries: entries, Source: SourceDirect}, nil
}

// parseHAPPJSON tries to parse data as sing-box JSON (array of happ.Server)
// and convert each server's outbounds to ProxyEntry values.
func (f *Fetcher) parseHAPPJSON(data []byte) ([]*ProxyEntry, error) {
	var servers []happ.Server
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("empty server list")
	}

	happEntries := happ.ConvertAllServers(servers)
	if len(happEntries) == 0 {
		return nil, fmt.Errorf("happ: no supported outbounds found in JSON")
	}

	entries := make([]*ProxyEntry, 0, len(happEntries))
	for _, he := range happEntries {
		entries = append(entries, &ProxyEntry{
			Protocol:    he.Protocol,
			Fingerprint: he.Fingerprint,
			TLSSecurity: he.TLSSecurity,
			Network:     he.Network,
			Outbound:    he.Outbound,
			RawURI:      he.Tag,
			Remarks:     he.Remarks,
			Country:     he.Country,
		})
	}

	GenerateTags(entries)
	return entries, nil
}

// dialDNSServers tries multiple DNS servers with per-server timeouts.
// Each attempt gets a fresh context to prevent one timeout from exhausting
// the parent context and short-circuiting remaining servers.
func dialDNSServers(ctx context.Context, defaultAddr string) (net.Conn, error) {
	dnsServers := []string{
		defaultAddr,  // system default (usually 127.0.0.1:53 on Keenetic)
		"8.8.8.8:53", // Google DNS
		"1.1.1.1:53", // Cloudflare DNS
		"8.8.4.4:53", // Google DNS secondary
	}

	var lastErr error
	for _, server := range dnsServers {
		// Fresh child context per attempt: if one server times out, the
		// parent context remains valid for the next server.
		dialCtx, dialCancel := context.WithTimeout(ctx, 5*time.Second)
		d := net.Dialer{}
		conn, err := d.DialContext(dialCtx, "udp", server)
		dialCancel()
		if err != nil {
			lastErr = err
			continue
		}
		return conn, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all DNS servers failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no DNS servers available")
}
