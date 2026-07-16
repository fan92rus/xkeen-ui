package utils

import (
	"encoding/json"
	"testing"
)

// --- JSONCtoJSON tests ---

func TestJSONCtoJSON_PlainJSON_PassesThrough(t *testing.T) {
	input := `{"name": "test", "value": 42}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The result should be valid JSON
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
}

func TestJSONCtoJSON_SingleLineComments_Stripped(t *testing.T) {
	input := `{
		// this is a comment
		"name": "test"
	}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
	// Verify the comment text is gone
	s := string(result)
	if containsSubstring(s, "this is a comment") {
		t.Errorf("comment text should be stripped, got: %s", s)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["name"] != "test" {
		t.Errorf("expected name=test, got %v", m["name"])
	}
}

func TestJSONCtoJSON_MultiLineComments_Stripped(t *testing.T) {
	input := `{
		/* this is a
		   multi-line comment */
		"name": "test"
	}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
	s := string(result)
	if containsSubstring(s, "multi-line") {
		t.Errorf("multi-line comment text should be stripped, got: %s", s)
	}
}

func TestJSONCtoJSON_TrailingComments(t *testing.T) {
	input := `{"name": "test"} // trailing comment`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(result)
	if containsSubstring(s, "trailing") {
		t.Errorf("trailing comment should be stripped, got: %s", s)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
}

func TestJSONCtoJSON_CommentsInURLPreserved(t *testing.T) {
	// Verify that // inside a string is NOT treated as a comment
	input := `{"url": "http://example.com"}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["url"] != "http://example.com" {
		t.Errorf("URL should be preserved, got: %v", m["url"])
	}
}

func TestJSONCtoJSON_EscapedQuotes_DontBreakCommentDetection(t *testing.T) {
	input := `{"text": "say \"hello\" // not a comment"}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	expected := `say "hello" // not a comment`
	if m["text"] != expected {
		t.Errorf("expected %q, got %q", expected, m["text"])
	}
}

func TestJSONCtoJSON_EmptyInput(t *testing.T) {
	result, err := JSONCtoJSON([]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty output, got: %q", result)
	}
}

func TestJSONCtoJSON_OnlyComments(t *testing.T) {
	input := `// just a comment`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not contain the comment text
	if containsSubstring(string(result), "just a comment") {
		t.Errorf("comment should be stripped, got: %q", result)
	}
}

func TestJSONCtoJSON_MixedCommentTypes(t *testing.T) {
	input := `{
		// single-line comment
		"name": "test",
		/* block
		   comment */
		"value": 42 // trailing
	}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["name"] != "test" || m["value"] != float64(42) {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestJSONCtoJSON_MultiLineCommentSpanningMultipleLines(t *testing.T) {
	input := `{
		/*
			line 1
			line 2
			line 3
		*/
		"key": "value"
	}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(result)
	if containsSubstring(s, "line 1") || containsSubstring(s, "line 2") || containsSubstring(s, "line 3") {
		t.Errorf("multi-line comment should be stripped, got: %s", s)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
}

func TestJSONCtoJSON_ConsecutiveSingleLineComments(t *testing.T) {
	input := `{
		// comment 1
		// comment 2
		// comment 3
		"key": "value"
	}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(result)
	if containsSubstring(s, "comment 1") || containsSubstring(s, "comment 2") || containsSubstring(s, "comment 3") {
		t.Errorf("consecutive comments should be stripped, got: %s", s)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
}

func TestJSONCtoJSON_CommentAtEOFWithoutNewline(t *testing.T) {
	input := `{"key": "value"} // comment at end`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(result)
	if containsSubstring(s, "comment at end") {
		t.Errorf("trailing comment without newline should be stripped, got: %s", s)
	}
	if !json.Valid(result) {
		t.Errorf("expected valid JSON, got: %s", result)
	}
}

func TestJSONCtoJSON_BackslashEscapeBeforeQuote(t *testing.T) {
	// `\\"` means escaped backslash followed by string-end quote
	input := `{"text": "end\\"} // real comment`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := string(result)
	if containsSubstring(s, "real comment") {
		t.Errorf("comment after string should be stripped, got: %s", s)
	}
}

func TestJSONCtoJSON_CommentInsideStringWithSlash(t *testing.T) {
	input := `{"path": "/opt/etc/*not a comment*/"}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if m["path"] != "/opt/etc/*not a comment*/" {
		t.Errorf("path inside string should be preserved, got: %v", m["path"])
	}
}

func TestJSONCtoJSON_NestedJSONWithComments(t *testing.T) {
	input := `{
		// top-level comment
		"outer": {
			// inner comment
			"inner": "value"
		}
	}`
	result, err := JSONCtoJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	outer, ok := m["outer"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected outer to be a map, got %T", m["outer"])
	}
	if outer["inner"] != "value" {
		t.Errorf("expected inner=value, got %v", outer["inner"])
	}
}

// --- ParseJSONC tests ---

func TestParseJSONC_ValidJSONC_ParsesToCorrectTypes(t *testing.T) {
	input := `{
		// a string value
		"name": "test",
		/* a number */
		"count": 42,
		"active": true,
		"nothing": null
	}`
	result, err := ParseJSONC([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["name"] != "test" {
		t.Errorf("expected name=test, got %v", m["name"])
	}
	if m["count"] != float64(42) {
		t.Errorf("expected count=42, got %v", m["count"])
	}
	if m["active"] != true {
		t.Errorf("expected active=true, got %v", m["active"])
	}
	if m["nothing"] != nil {
		t.Errorf("expected nothing=nil, got %v", m["nothing"])
	}
}

func TestParseJSONC_InvalidJSON_ReturnsError(t *testing.T) {
	input := `{invalid json}`
	_, err := ParseJSONC([]byte(input))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseJSONC_Array(t *testing.T) {
	input := `[
		// first item
		1,
		2,
		3 /* third */
	]`
	result, err := ParseJSONC([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected slice, got %T", result)
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0] != float64(1) || arr[1] != float64(2) || arr[2] != float64(3) {
		t.Errorf("unexpected values: %v", arr)
	}
}

func TestParseJSONC_ComplexNested(t *testing.T) {
	input := `{
		/* Configuration */
		"server": {
			"host": "localhost", // host
			"port": 8080
		},
		"routes": [
			{"path": "/", "handler": "index"},
			{"path": "/api", "handler": "api"}
		]
	}`
	result, err := ParseJSONC([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	server, ok := m["server"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected server to be a map, got %T", m["server"])
	}
	if server["host"] != "localhost" {
		t.Errorf("expected host=localhost, got %v", server["host"])
	}
	routes, ok := m["routes"].([]interface{})
	if !ok {
		t.Fatalf("expected routes to be a slice, got %T", m["routes"])
	}
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
}

func TestParseJSONC_EmptyObject(t *testing.T) {
	input := `{}`
	result, err := ParseJSONC([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d keys", len(m))
	}
}

func TestParseJSONC_EmptyInput(t *testing.T) {
	_, err := ParseJSONC([]byte{})
	if err == nil {
		t.Error("expected error for empty input (not valid JSON)")
	}
}

// helper
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
