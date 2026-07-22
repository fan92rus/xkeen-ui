package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// === SSEWriter Tests ===

func TestNewSSEWriter_SetsHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, ok := NewSSEWriter(rec)
	if !ok {
		t.Fatal("expected ok=true for httptest.ResponseRecorder (implements Flusher)")
	}
	if sse == nil {
		t.Fatal("expected non-nil SSEWriter")
	}

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if conn := rec.Header().Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}
}

func TestSSEWriter_SendEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, _ := NewSSEWriter(rec)

	type payload struct {
		Msg string `json:"msg"`
	}
	err := sse.Send("progress", payload{Msg: "hello"})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	body := rec.Body.String()
	// SSE wire format: "event: <name>\ndata: <json>\n\n"
	want := "event: progress\ndata: {\"msg\":\"hello\"}\n\n"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestSSEWriter_SendMultipleEvents(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, _ := NewSSEWriter(rec)

	_ = sse.Send("progress", map[string]int{"percent": 50})
	_ = sse.Send("complete", map[string]bool{"success": true})

	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected non-empty body after two events")
	}

	// Both event names should appear
	if !strings.Contains(body, "event: progress") {
		t.Errorf("body missing 'event: progress': %q", body)
	}
	if !strings.Contains(body, "event: complete") {
		t.Errorf("body missing 'event: complete': %q", body)
	}

	// Verify JSON payloads parse
	type pct struct {
		Percent int `json:"percent"`
	}
	var p pct
	raw := extractDataLine(body, "progress")
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Errorf("failed to parse progress data %q: %v", raw, err)
	}
	if p.Percent != 50 {
		t.Errorf("percent = %d, want 50", p.Percent)
	}
}

func TestNewSSEWriter_SetsExtraHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	sse, ok := NewSSEWriter(rec, "Access-Control-Allow-Origin: *")
	if !ok {
		t.Fatal("expected ok=true")
	}
	_ = sse

	if ao := rec.Header().Get("Access-Control-Allow-Origin"); ao != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", ao)
	}
}

// extractDataLine returns the data payload JSON for a given event name.
func extractDataLine(body, event string) string {
	marker := "event: " + event + "\n"
	idx := strings.Index(body, marker)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(marker):]
	dataIdx := strings.Index(rest, "data: ")
	if dataIdx < 0 {
		return ""
	}
	rest = rest[dataIdx+len("data: "):]
	endIdx := strings.Index(rest, "\n")
	if endIdx < 0 {
		return rest
	}
	return rest[:endIdx]
}
