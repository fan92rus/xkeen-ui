package subscription

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetcher_BasicFetch(t *testing.T) {
	// Create test subscription content (base64-encoded vless URIs)
	lines := []string{
		"vless://a1b2c3d4-e5f6-0000-abcd-ef1234567890@1.2.3.4:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=example.com&fp=chrome&pbk=testkey&sid=abcd1234#%F0%9F%87%A9%F0%9F%87%AA%20Germany",
		"vless://a1b2c3d4-e5f6-0001-abcd-ef1234567890@5.6.7.8:8443?encryption=none&flow=xtls-rprx-vision&type=tcp&security=reality&sni=example.com&fp=edge&pbk=testkey&sid=abcd1234#%F0%9F%87%B3%F0%9F%87%B1%20Netherlands",
	}
	content := base64.StdEncoding.EncodeToString([]byte(
		lines[0] + "\n" + lines[1] + "\n",
	))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "Hiddify" {
			t.Errorf("expected User-Agent 'Hiddify', got %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(content))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	entries, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Country != "DE" {
		t.Errorf("expected country DE, got %q", entries[0].Country)
	}
	if entries[1].Country != "NL" {
		t.Errorf("expected country NL, got %q", entries[1].Country)
	}

	if entries[0].Tag == "" || entries[1].Tag == "" {
		t.Error("expected tags to be generated")
	}
}

func TestFetcher_PlainTextResponse(t *testing.T) {
	lines := "vless://uuid1@1.2.3.4:443?encryption=none&type=tcp&security=none#Test1\n" +
		"vless://uuid2@5.6.7.8:443?encryption=none&type=tcp&security=none#Test2\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(lines))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	entries, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestFetcher_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	fetcher := NewFetcher()
	_, err := fetcher.Fetch(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

func TestFetcher_EmptyURL(t *testing.T) {
	fetcher := NewFetcher()
	_, err := fetcher.Fetch(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestFetcher_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	fetcher := NewFetcher()
	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("expected error for context timeout")
	}
}

func TestFetcher_InvalidSubscriptionContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is not a valid subscription"))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	_, err := fetcher.Fetch(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error for invalid content")
	}
}

func TestFetcher_MixedProtocols(t *testing.T) {
	lines := "vless://uuid1@1.2.3.4:443?encryption=none&type=tcp&security=none#VLESS\n" +
		"trojan://password@5.6.7.8:443?security=tls&type=tcp#TROJAN\n" +
		"invalid-protocol://something\n" // should be skipped

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(lines))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	entries, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries (1 skipped), got %d", len(entries))
	}

	if entries[0].Protocol != "vless" {
		t.Errorf("expected vless, got %q", entries[0].Protocol)
	}
	if entries[1].Protocol != "trojan" {
		t.Errorf("expected trojan, got %q", entries[1].Protocol)
	}
}

func TestFetcher_WithURLSafeBase64(t *testing.T) {
	lines := "vless://uuid1@1.2.3.4:443?encryption=none&type=tcp&security=none#Test\n"
	// Use URL-safe base64 (no +/ with -_)
	content := base64.URLEncoding.EncodeToString([]byte(lines))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(content))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	entries, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}
