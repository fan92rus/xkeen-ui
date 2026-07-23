package happ

import (
	"strings"
	"testing"
)

func TestSwapBlockHalves_Roundtrip(t *testing.T) {
	original := []byte("ABCDabcd1234")
	swapped := swapBlockHalves(original)
	// Every 4-byte block: [A,B,C,D] → [C,D,A,B]
	// "ABCD" → "CDAB", "abcd" → "cdab", "1234" → "3412"
	expected := []byte("CDABcdab3412")
	if string(swapped) != string(expected) {
		t.Errorf("swapBlockHalves(%q) = %q, want %q", original, swapped, expected)
	}
	// Must be self-inverse
	twice := swapBlockHalves(swapped)
	if string(twice) != string(original) {
		t.Errorf("swapBlockHalves(self-inverse) = %q, want %q", twice, original)
	}
}

func TestSwapBlockHalves_OddLength(t *testing.T) {
	// Length not multiple of 4 — trailing bytes must be unchanged.
	in := []byte("ABCDE")
	got := swapBlockHalves(in)
	// "ABCD" → "CDAB", "E" stays
	if string(got) != "CDABE" {
		t.Errorf("swapBlockHalves(%q) = %q, want %q", in, got, "CDABE")
	}
}

func TestSwapAdjacent_Roundtrip(t *testing.T) {
	in := []byte("ABCDabcd")
	swapped := swapAdjacent(in)
	// [A,B,C,D,a,b,c,d] → [B,A,D,C,b,a,d,c]
	if string(swapped) != "BADCbadc" {
		t.Errorf("swapAdjacent(%q) = %q, want %q", in, swapped, "BADCbadc")
	}
	// Self-inverse
	twice := swapAdjacent(swapped)
	if string(twice) != string(in) {
		t.Errorf("swapAdjacent(self-inverse) = %q, want %q", twice, in)
	}
}

func TestSwapAdjacent_OddLength(t *testing.T) {
	in := []byte("ABC")
	got := swapAdjacent(in)
	// [A,B] → [B,A], C stays
	if string(got) != "BAC" {
		t.Errorf("swapAdjacent(%q) = %q, want %q", in, got, "BAC")
	}
}

func TestB64Decode_Standard(t *testing.T) {
	// "hello" base64 = "aGVsbG8="
	data, err := b64Decode("aGVsbG8=")
	if err != nil {
		t.Fatalf("b64Decode failed: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("b64Decode = %q, want %q", string(data), "hello")
	}
}

func TestB64Decode_URLSafe(t *testing.T) {
	// URL-safe variant uses - instead of +, _ instead of /
	data, err := b64Decode("aGVsbG8") // no padding
	if err != nil {
		t.Fatalf("b64Decode failed: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("b64Decode = %q, want %q", string(data), "hello")
	}
}

func TestB64Decode_Empty(t *testing.T) {
	data, err := b64Decode("")
	if err != nil {
		t.Fatalf("b64Decode failed: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("b64Decode empty = %d bytes, want 0", len(data))
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}
	_, err = d.Decrypt("crypt5/short")
	if err == nil {
		t.Fatal("expected error for short payload")
	}
	if !strings.Contains(err.Error(), "too short") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecrypt_UnknownMarker(t *testing.T) {
	// Build a payload where the marker (first 4 + last 4 bytes)
	// after swapBlockHalves is "XXXXXXXX".
	// 8 bytes total, alphabet chars so swapBlockHalves is a no-op.
	payload := "XXXXXXXX"
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}
	_, err = d.Decrypt("crypt5/" + payload)
	if err == nil {
		t.Fatal("expected error for unknown marker")
	}
	if !strings.Contains(err.Error(), "unknown crypt5 marker") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecrypt_EmptyLink(t *testing.T) {
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}
	_, err = d.Decrypt("crypt5/")
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestDecrypt_NoCrypt5Prefix(t *testing.T) {
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}
	// Without "crypt5/" prefix — treated as raw payload.
	_, err = d.Decrypt("")
	if err == nil {
		t.Fatal("expected error for empty raw payload")
	}
}

func TestNewDecryptorEmbedded_Success(t *testing.T) {
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}
	if d == nil {
		t.Fatal("NewDecryptorEmbedded returned nil")
	}
}
