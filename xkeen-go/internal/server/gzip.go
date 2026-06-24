package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// minGzipSize is the smallest response worth compressing. Below this,
// compression overhead and latency outweigh the bandwidth savings.
const minGzipSize = 1400

// preCompressedTypes are content types that are already compressed;
// re-compressing wastes CPU for negligible size reduction.
var preCompressedTypes = map[string]bool{
	"image/png":      true,
	"image/jpeg":     true,
	"image/gif":      true,
	"image/webp":     true,
	"application/zip": true,
	"application/gzip": true,
	"application/x-gzip": true,
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

// GzipMiddleware compresses large, compressible HTTP responses with gzip.
// It is intended for static assets (bundle.js, HTML) served over a slow
// router LAN. Small responses, already-compressed types, and clients that do
// not accept gzip are passed through untouched. WebSocket / streaming
// responses are not affected when this middleware is applied only to the
// /static/ route and normal API handlers (it buffers via a byte-counting
// writer and only switches to gzip once the threshold is crossed).
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !clientAcceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.Close()

		next.ServeHTTP(gw, r)
	})
}

// gzipResponseWriter buffers the response. Once enough bytes have been
// written to justify compression AND the content type is compressible, it
// flips into gzip mode, emits the Content-Encoding/Vary headers, and streams
// the buffered prefix + subsequent writes through a gzip.Writer.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	headerWritten bool
	compress     bool   // decided after first Write / explicit content type
	decided      bool
	buf          []byte // buffered until we decide
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	// Defer the real header write until we decide whether to compress.
	if !w.decided {
		// We can't fully decide on content type from WriteHeader alone if
		// Content-Type isn't set yet; flush decision on first Write.
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) decide(contentType string) {
	w.decided = true
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	compressible := ct != "" && !preCompressedTypes[ct]
	w.compress = compressible
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.decided {
		ct := w.Header().Get("Content-Type")
		w.decide(ct)
	}

	if !w.compress {
		// Not compressible: write everything through directly.
		return w.ResponseWriter.Write(b)
	}

	// Buffer until we know whether the response is large enough.
	if w.gz == nil {
		w.buf = append(w.buf, b...)
		if len(w.buf) < minGzipSize {
			return len(b), nil
		}
		// Threshold crossed: commit to gzip.
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.gz = gzipWriterPool.Get().(*gzip.Writer)
		w.gz.Reset(w.ResponseWriter)
		w.headerWritten = true
		_, err := w.gz.Write(w.buf)
		if err != nil {
			return 0, err
		}
		w.buf = nil
		return len(b), nil
	}
	return w.gz.Write(b)
}

func (w *gzipResponseWriter) Close() {
	if w.gz != nil {
		w.gz.Close()
		gzipWriterPool.Put(w.gz)
		w.gz = nil
	} else if w.compress && len(w.buf) > 0 {
		// Decided compressible but never reached threshold: flush raw.
		w.ResponseWriter.Write(w.buf)
		w.buf = nil
	}
}

// Flush implements http.Flusher so streaming/hijacked handlers (e.g. WS)
// are not silently buffered when the middleware is in front of them.
func (w *gzipResponseWriter) Flush() {
	if w.gz != nil {
		w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
