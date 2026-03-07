package utils

import (
	"encoding/json"
	"testing"
)

func TestJSONCtoJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple single-line comment with newline",
			input:    "{ \"key\": \"value\" // this is a comment\n }",
			expected: "{ \"key\": \"value\" \n }",
		},
		{
			name:     "Multi-line comment",
			input:    "{ \"key\": /* comment */ \"value\" }",
			expected: "{ \"key\":  \"value\" }",
		},
		{
			name:     "Comment inside string should be preserved",
			input:    "{ \"key\": \"value // not a comment\" }",
			expected: "{ \"key\": \"value // not a comment\" }",
		},
		{
			name:     "Multi-line comment inside string should be preserved",
			input:    "{ \"key\": \"value /* in a comment */\" }",
			expected: "{ \"key\": \"value /* in a comment */\" }",
		},
		{
			name:     "Trailing comment with newline",
			input:    "{ \"key\": \"value\" }\n// trailing",
			expected: "{ \"key\": \"value\" }\n",
		},
		{
			name: "Multiple comments",
			input: "{\n\t\t\t\t// single line comment\n\t\t\t\t\"key1\": \"value1\",\n\t\t\t\t/* multi\n\t\t\t\t   line\n                   comment */\n\t\t\t\t\"key2\": \"value2\" // trailing comment\n\t\t\t}",
			expected: "{\n\t\t\t\t\n\t\t\t\t\"key1\": \"value1\",\n\t\t\t\t\n\t\t\t\t\"key2\": \"value2\" \n\t\t\t}",
		},
		{
			name:     "Valid JSON without comments",
			input:    "{ \"key\": \"value\", \"number\": 42, \"bool\": true }",
			expected: "{ \"key\": \"value\", \"number\": 42, \"bool\": true }",
		},
		{
			name:     "Escaped quote in string",
			input:    "{ \"key\": \"value with \\\" quote\" // comment\n }",
			expected: "{ \"key\": \"value with \\\" quote\" \n }",
		},
		{
			name:     "Escaped backslash before quote",
			input:    "{ \"key\": \"path\\\\file\" // comment\n }",
			expected: "{ \"key\": \"path\\\\file\" \n }",
		},
		{
			name: "Complex nested structure with comments",
			input: "{\n\t\t\t\t// Top level comment\n\t\t\t\t\"object\": {\n\t\t\t\t\t/* Nested comment */\n\t\t\t\t\t\"nested\": \"value\"\n\t\t\t\t},\n\t\t\t\t\"array\": [1, 2, 3], // Array comment\n\t\t\t\t\"string\": \"text\" /* trailing block */\n\t\t\t}",
			expected: "{\n\t\t\t\t\n\t\t\t\t\"object\": {\n\t\t\t\t\t\n\t\t\t\t\t\"nested\": \"value\"\n\t\t\t\t},\n\t\t\t\t\"array\": [1, 2, 3], \n\t\t\t\t\"string\": \"text\" \n\t\t\t}",
		},
		{
			name:     "Comment at beginning of file with newline",
			input:    "// Comment at start\n{ \"key\": \"value\" }",
			expected: "\n{ \"key\": \"value\" }",
		},
		{
			name:     "Multi-line comment at beginning",
			input:    "/* Start comment */{ \"key\": \"value\" }",
			expected: "{ \"key\": \"value\" }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := JSONCtoJSON([]byte(tt.input))
			if err != nil {
				t.Fatalf("JSONCtoJSON() error = %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("JSONCtoJSON()\n  got:      %q\n  expected: %q", string(result), tt.expected)
			}
		})
	}
}

func TestParseJSONC(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]interface{}
	}{
		{
			name:  "Simple object with comment",
			input: "{ \"key\": \"value\" // comment\n}",
			expected: map[string]interface{}{
				"key": "value",
			},
		},
		{
			name:  "Object with multiple comments",
			input: "{ \"name\": \"test\", /* comment */ \"value\": 42 }",
			expected: map[string]interface{}{
				"name":  "test",
				"value": float64(42),
			},
		},
		{
			name:  "Nested object with comments",
			input: "{ \"outer\": { \"inner\": \"value\" /* inner comment */ } // outer comment\n}",
			expected: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "value",
				},
			},
		},
		{
			name:  "Array with comments",
			input: "{ \"items\": [1, 2, 3] // array comment\n}",
			expected: map[string]interface{}{
				"items": []interface{}{float64(1), float64(2), float64(3)},
			},
		},
		{
			name:  "String with comment-like content",
			input: "{ \"url\": \"https://example.com\" // real comment\n}",
			expected: map[string]interface{}{
				"url": "https://example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseJSONC([]byte(tt.input))
			if err != nil {
				t.Fatalf("ParseJSONC() error = %v", err)
			}

			// Convert expected to JSON and back for comparison
			expectedJSON, _ := json.Marshal(tt.expected)
			resultJSON, _ := json.Marshal(result)

			if string(resultJSON) != string(expectedJSON) {
				t.Errorf("ParseJSONC()\n  got:      %s\n  expected: %s", string(resultJSON), string(expectedJSON))
			}
		})
	}
}

func TestParseJSONCArray(t *testing.T) {
	input := "[\n\t\t// First item\n\t\t\"item1\",\n\t\t/* Second item */\n\t\t\"item2\",\n\t\t\"item3\" // Last item\n\t]"

	result, err := ParseJSONC([]byte(input))
	if err != nil {
		t.Fatalf("ParseJSONC() error = %v", err)
	}

	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("Expected array, got %T", result)
	}

	if len(arr) != 3 {
		t.Errorf("Expected 3 items, got %d", len(arr))
	}

	expected := []string{"item1", "item2", "item3"}
	for i, v := range arr {
		if v != expected[i] {
			t.Errorf("Item %d: expected %s, got %s", i, expected[i], v)
		}
	}
}

func TestJSONCWithEscapedQuotes(t *testing.T) {
	input := "{\n\t\t\"message\": \"He said \\\"hello\\\" // not a comment\",\n\t\t\"path\": \"C:\\\\Users\\\\test\" // real comment\n\t}"

	result, err := ParseJSONC([]byte(input))
	if err != nil {
		t.Fatalf("ParseJSONC() error = %v", err)
	}

	obj, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map, got %T", result)
	}

	expectedMessage := `He said "hello" // not a comment`
	if obj["message"] != expectedMessage {
		t.Errorf("message: expected %q, got %q", expectedMessage, obj["message"])
	}

	expectedPath := `C:\Users\test`
	if obj["path"] != expectedPath {
		t.Errorf("path: expected %q, got %q", expectedPath, obj["path"])
	}
}
