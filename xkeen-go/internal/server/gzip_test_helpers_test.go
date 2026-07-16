package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// httpHandler returns a handler writing s as the body with a default
// text/plain Content-Type.
func httpHandler(s string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(s))
	})
}

// gzipHandlerWithContentType returns a handler that sets the given
// Content-Type then writes body.
func gzipHandlerWithContentType(body []byte, contentType string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	})
}

// newRequest builds a GET request carrying Accept-Encoding: gzip when
// acceptGzip is true.
func newRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	return req
}

func newRequestAcceptingGzip(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, http.NoBody)
	req.Header.Set("Accept-Encoding", "gzip")
	return req
}

// gunzip decodes a gzip-compressed byte slice (used to verify responses).
func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	return string(out)
}

// makeFakePNG returns n bytes of plausible (already-compressed-looking) data.
func makeFakePNG(n int) []byte {
	b := make([]byte, n)
	// PNG signature + filler; values are arbitrary but non-repetitive-ish.
	for i := range b {
		b[i] = byte(i * 7)
	}
	copy(b[0:], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	return b
}
