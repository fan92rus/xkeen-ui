package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// SSEWriter wraps an http.ResponseWriter for Server-Sent Events (SSE) streaming.
// It centralizes the SSE boilerplate (headers, flusher, event framing) that was
// previously duplicated across service, update, and install handlers.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter sets the standard SSE response headers on w and returns a writer
// ready to send events. If w does not implement http.Flusher it writes a 500
// error and returns (nil, false).
//
// extraHeaders (optional) are additional "Key: Value" header lines to set, e.g.
// "Access-Control-Allow-Origin: *" for cross-origin update endpoints.
func NewSSEWriter(w http.ResponseWriter, extraHeaders ...string) (*SSEWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return nil, false
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, h := range extraHeaders {
		if key, val, ok := strings.Cut(h, ":"); ok {
			w.Header().Set(strings.TrimSpace(key), strings.TrimSpace(val))
		}
	}

	return &SSEWriter{w: w, flusher: flusher}, true
}

// Send writes a single SSE event with the given name and JSON-serialized data,
// then flushes the buffer so the client receives it immediately. The error
// (typically from a disconnected client) is returned so the caller can stop the
// event loop.
func (s *SSEWriter) Send(event string, data interface{}) error {
	jsonData, _ := json.Marshal(data)
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, jsonData); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendRaw writes a pre-formatted SSE frame without JSON serialization.
// Useful when the caller has already serialized the data.
func (s *SSEWriter) SendRaw(event, data string) error {
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
