package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// minGzipSize is the smallest response worth compressing. Below this,
// compression overhead and latency outweigh the bandwidth savings.
const minGzipSize = 1400

// preCompressedTypes are content types that are already compressed; re-compressing
// them wastes CPU for negligible size reduction.
var preCompressedTypes = map[string]bool{
	"image/png":           true,
	"image/jpeg":          true,
	"image/gif":           true,
	"image/webp":          true,
	"application/zip":     true,
	"application/gzip":    true,
	"application/x-gzip":  true,
}

// gzipWriterPool reuses gzip writers to avoid allocations per request.
var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.DefaultCompression)
		return w
	},
}

// clientAcceptsGzip reports whether the request advertises gzip support.
func clientAcceptsGzip(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip")
}

// compressibleType reports whether a Content-Type value should be gzipped.
func compressibleType(contentType string) bool {
	if contentType == "" {
		return false
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if preCompressedTypes[ct] {
		return false
	}
	// Compress text-based types (css, js, json, html, svg, plain, xml, ...).
	// Everything else (unknown/binary) is left alone.
	return strings.HasPrefix(ct, "text/") ||
		ct == "application/javascript" ||
		strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml") ||
		ct == "image/svg+xml"
}

// GzipMiddleware compresses large, compressible HTTP responses with gzip.
//
// It is scoped to the /static/ route. The response body is fully buffered so
// the gzip decision (size threshold + content type) and the resulting
// Content-Encoding/Vary/Content-Length headers are all emitted BEFORE the
// status line and headers hit the wire. This is essential: setting
// Content-Encoding after WriteHeader has already been sent is a no-op and
// corrupts the response (client receives gzip bytes it never agreed to).
//
// Because it buffers, it must NOT wrap streaming endpoints (WebSocket / SSE).
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !clientAcceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipResponseWriter{
			ResponseWriter: w,
			buf:            &bytes.Buffer{},
		}
		defer gw.flush()
		next.ServeHTTP(gw, r)
	})
}

// gzipResponseWriter buffers the entire response body. The gzip decision and
// all headers are emitted atomically in flush(), guaranteeing Content-Encoding
// is set before WriteHeader hits the network.
type gzipResponseWriter struct {
	http.ResponseWriter
	statusCode int
	headerSent bool
	buf        *bytes.Buffer
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.headerSent {
		return
	}
	w.statusCode = code
	// Do NOT forward yet: defer until flush() so we can add Content-Encoding.
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.headerSent {
		// Remember the intended status (default 200) but keep buffering.
		if w.statusCode == 0 {
			w.statusCode = http.StatusOK
		}
	}
	return w.buf.Write(b)
}

// flush emits status + headers + body. If the body is large enough and the
// content type is compressible, it is gzipped and Content-Encoding/Vary are
// set (Content-Length removed since the gzip size differs). Otherwise the
// buffered body is written verbatim.
func (w *gzipResponseWriter) flush() {
	if w.headerSent {
		return
	}
	w.headerSent = true
	status := w.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	body := w.buf.Bytes()
	contentType := w.Header().Get("Content-Type")

	if len(body) >= minGzipSize && compressibleType(contentType) {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.ResponseWriter.WriteHeader(status)
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w.ResponseWriter)
		_, _ = gz.Write(body)
		_ = gz.Close()
		gzipWriterPool.Put(gz)
		return
	}

	// Pass through uncompressed. Fix Content-Length to the buffered size so it
	// stays correct regardless of what the inner handler assumed.
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.ResponseWriter.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.ResponseWriter.Write(body)
	}
}

// Flush implements http.Flusher so handlers that explicitly flush are not
// silently broken. Note: because this writer buffers, flushing forces the
// gzip decision with whatever has been written so far (uncompressed path).
func (w *gzipResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
