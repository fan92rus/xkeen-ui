package subscription

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Fetcher downloads and parses subscription content from a URL.
type Fetcher struct {
	client *http.Client
}

// NewFetcher creates a Fetcher with a 30-second timeout and Hiddify User-Agent.
func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
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
