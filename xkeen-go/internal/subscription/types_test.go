package subscription

import (
	"encoding/json"
	"testing"
)

func TestFlexibleInt_UnmarshalJSON_Number(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte("42"), &fi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != 42 {
		t.Errorf("expected 42, got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_Zero(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte("0"), &fi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != 0 {
		t.Errorf("expected 0, got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_Negative(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte("-5"), &fi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != -5 {
		t.Errorf("expected -5, got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_StringNumber(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte(`"120"`), &fi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != 120 {
		t.Errorf("expected 120, got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_StringZero(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte(`"0"`), &fi)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != 0 {
		t.Errorf("expected 0, got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_StringNonNumber(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte(`"abc"`), &fi)
	if err == nil {
		t.Error("expected error for non-numeric string")
	}
}

func TestFlexibleInt_UnmarshalJSON_FloatString(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte(`"3.14"`), &fi)
	// Sscanf %d parses "3" from "3.14" and stops — it returns 3, no error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != 3 {
		t.Errorf("expected 3 (truncated), got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_Bool(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte("true"), &fi)
	if err == nil {
		t.Error("expected error for boolean value")
	}
}

func TestFlexibleInt_UnmarshalJSON_Null(t *testing.T) {
	var fi FlexibleInt
	err := json.Unmarshal([]byte("null"), &fi)
	// json.Unmarshal(null, *int) sets to zero and returns nil
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int(fi) != 0 {
		t.Errorf("expected 0 (null -> zero), got %d", fi)
	}
}

func TestFlexibleInt_UnmarshalJSON_InStruct(t *testing.T) {
	type testStruct struct {
		Interval FlexibleInt `json:"interval"`
	}
	var s testStruct

	// From number
	err := json.Unmarshal([]byte(`{"interval": 30}`), &s)
	if err != nil {
		t.Fatalf("number: unexpected error: %v", err)
	}
	if int(s.Interval) != 30 {
		t.Errorf("expected 30, got %d", s.Interval)
	}

	// From string
	err = json.Unmarshal([]byte(`{"interval": "60"}`), &s)
	if err != nil {
		t.Fatalf("string: unexpected error: %v", err)
	}
	if int(s.Interval) != 60 {
		t.Errorf("expected 60, got %d", s.Interval)
	}
}
