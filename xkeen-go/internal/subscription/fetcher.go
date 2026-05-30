package subscription

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Fetcher downloads and parses subscription content from a URL.
type Fetcher struct {
	client *http.Client
}

// NewFetcher creates a Fetcher with a 30-second timeout, Hiddify User-Agent,
// and a custom DNS resolver that falls back to public DNS if the system
// resolver is unavailable (common on Keenetic routers where local DNS
// may be intercepted by xray).
func NewFetcher() *Fetcher {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				// Try system DNS first, then fallback to public DNS servers.
				dnsServers := []string{
					address,          // system default (usually 127.0.0.1:53 on Keenetic)
					"8.8.8.8:53",    // Google DNS
					"1.1.1.1:53",    // Cloudflare DNS
					"8.8.4.4:53",    // Google DNS secondary
				}

				var lastErr error
				for _, server := range dnsServers {
					d := net.Dialer{Timeout: 5 * time.Second}
					conn, err := d.DialContext(ctx, "udp", server)
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
			},
		},
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:  10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}

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

// Fetch downloads the subscription from url, decodes base64 if needed,
// and parses each share URI into a ProxyEntry.
func (f *Fetcher) Fetch(ctx context.Context, url string) ([]*ProxyEntry, error) {
	if url == "" {
		return nil, fmt.Errorf("subscription URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Hiddify")
	req.Header.Set("Accept", "*/*")

	resp, err := f.client.Do(req)
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
