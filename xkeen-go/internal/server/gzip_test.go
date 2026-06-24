package server

import (
	"net/http/httptest"
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
