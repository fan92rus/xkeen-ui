package server

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestGzipMiddleware_CompressesResponse(t *testing.T) {
	// Large payload that gzip will meaningfully compress. The middleware
	// only compresses responses above the minimum size threshold.
	payload := strings.Repeat("hello world. ", 500) // ~6.5 KB
	handler := GzipMiddleware(httpHandler(payload))

	rec := httptest.NewRecorder()
	req := newRequestAcceptingGzip("GET", "/static/dist/bundle.js")

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", rec.Header().Get("Content-Encoding"))
	}
	if v := rec.Header().Get("Vary"); !strings.Contains(v, "Accept-Encoding") {
		t.Fatalf("expected Vary to include Accept-Encoding, got %q", v)
	}

	decoded := gunzip(t, rec.Body.Bytes())
	if decoded != payload {
		t.Fatalf("decompressed body mismatch (got %d bytes, want %d)", len(decoded), len(payload))
	}
}

func TestGzipMiddleware_SkipsWhenClientDoesNotAcceptGzip(t *testing.T) {
	payload := strings.Repeat("hello world. ", 500)
	handler := GzipMiddleware(httpHandler(payload))

	rec := httptest.NewRecorder()
	req := newRequest("GET", "/static/dist/bundle.js", nil) // no Accept-Encoding

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("response was gzip-compressed despite client not accepting gzip")
	}
	if rec.Body.String() != payload {
		t.Fatalf("expected raw body when no gzip, got %d bytes", rec.Body.Len())
	}
}

func TestGzipMiddleware_SkipsSmallResponses(t *testing.T) {
	// Tiny responses are not worth compressing (overhead + latency).
	payload := "tiny"
	handler := GzipMiddleware(httpHandler(payload))

	rec := httptest.NewRecorder()
	req := newRequestAcceptingGzip("GET", "/static/dist/bundle.js")

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("small response was gzip-compressed; should pass through uncompressed")
	}
	if rec.Body.String() != payload {
		t.Fatalf("expected raw small body, got %q", rec.Body.String())
	}
}

func TestGzipMiddleware_SkipsAlreadyCompressedTypes(t *testing.T) {
	// PNG is already compressed; re-compressing wastes CPU for ~0 gain.
	payload := makeFakePNG(8192) // 8KB of "png" data
	handler := gzipHandlerWithContentType(payload, "image/png")

	rec := httptest.NewRecorder()
	req := newRequestAcceptingGzip("GET", "/static/logo.png")

	GzipMiddleware(handler).ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("image/png was gzip-compressed; pre-compressed types should pass through")
	}
}

// --- Real-network integration tests ---
// The recorder-based tests above CANNOT catch the WriteHeader-ordering bug
// (an httptest.ResponseRecorder keeps accepting Header().Set() even after
// WriteHeader, unlike a real net/http conn). These tests use httptest.NewServer
// so headers are committed to the wire exactly as a browser sees them.

// realServer wraps a handler in GzipMiddleware and serves it over real TCP.
func realServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	return httptest.NewServer(GzipMiddleware(h))
}

func getGzipBody(t *testing.T, url string) (status int, contentEncoding, vary, contentLength string, body []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Encoding"), resp.Header.Get("Vary"), resp.Header.Get("Content-Length"), b
}

// Regression: a large text response MUST advertise Content-Encoding: gzip AND
// be decompressible. The old implementation forgot Content-Encoding (set after
// WriteHeader had hit the wire) so browsers got garbage.
func TestGzipMiddleware_RealServer_LargeTextIsGzipped(t *testing.T) {
	payload := strings.Repeat("hello world. ", 500)
	srv := realServer(t, httpHandler(payload))
	defer srv.Close()

	status, ce, vary, _, body := getGzipBody(t, srv.URL)
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip (browser would see garbage otherwise)", ce)
	}
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("Vary = %q, want Accept-Encoding", vary)
	}
	decoded := gunzip(t, body)
	if decoded != payload {
		t.Fatalf("decompressed body mismatch: got %d bytes, want %d", len(decoded), len(payload))
	}
}

// Regression: small text responses are served uncompressed with a correct
// Content-Length matching the actual bytes (not a stale length).
func TestGzipMiddleware_RealServer_SmallTextPassesThrough(t *testing.T) {
	payload := "tiny response"
	srv := realServer(t, httpHandler(payload))
	defer srv.Close()

	status, ce, _, cl, body := getGzipBody(t, srv.URL)
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if ce == "gzip" {
		t.Fatal("small response was compressed; expected pass-through")
	}
	if string(body) != payload {
		t.Fatalf("body = %q, want %q", string(body), payload)
	}
	if cl != "" && cl != strconv.Itoa(len(payload)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(payload))
	}
}

// Regression: a CSS file (the real-world failure) served over the network
// must decompress to valid CSS.
func TestGzipMiddleware_RealServer_CSSDecompresses(t *testing.T) {
	css := strings.Repeat(".foo { color: red; background: #1b2434; }\n", 200) // ~9KB
	h := gzipHandlerWithContentType([]byte(css), "text/css; charset=utf-8")
	srv := realServer(t, h)
	defer srv.Close()

	status, ce, _, _, body := getGzipBody(t, srv.URL)
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if ce != "gzip" {
		t.Fatalf("CSS Content-Encoding = %q, want gzip", ce)
	}
	decoded := gunzip(t, body)
	if decoded != css {
		t.Fatalf("CSS decompressed mismatch: got %d bytes, want %d", len(decoded), len(css))
	}
}

// Sanity: an already-compressed image is served uncompressed (no double gzip).
func TestGzipMiddleware_RealServer_PNGNotRecompressed(t *testing.T) {
	png := makeFakePNG(8192)
	h := gzipHandlerWithContentType(png, "image/png")
	srv := realServer(t, h)
	defer srv.Close()

	status, ce, _, _, body := getGzipBody(t, srv.URL)
	if status != 200 {
		t.Fatalf("status = %d", status)
	}
	if ce == "gzip" {
		t.Fatal("PNG was re-compressed; expected pass-through")
	}
	if !bytes.Equal(body, png) {
		t.Fatalf("PNG body altered: got %d bytes, want %d", len(body), len(png))
	}
}
