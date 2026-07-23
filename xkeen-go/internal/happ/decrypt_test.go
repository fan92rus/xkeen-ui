package happ

import (
	"bytes"
	"strings"
	"testing"
)

func TestSwapBlockHalves_Roundtrip(t *testing.T) {
	original := []byte("ABCDabcd1234")
	swapped := swapBlockHalves(original)
	// Every 4-byte block: [A,B,C,D] → [C,D,A,B]
	// "ABCD" → "CDAB", "abcd" → "cdab", "1234" → "3412"
	expected := []byte("CDABcdab3412")
	if !bytes.Equal(swapped, expected) {
		t.Errorf("swapBlockHalves(%q) = %q, want %q", original, swapped, expected)
	}
	// Must be self-inverse
	twice := swapBlockHalves(swapped)
	if !bytes.Equal(twice, original) {
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
	if !bytes.Equal(twice, in) {
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
	t.Parallel()
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}
	if d == nil {
		t.Fatal("NewDecryptorEmbedded returned nil")
	}
}

func TestLoadCrypt1to4Keys(t *testing.T) {
	t.Parallel()
	// loadCrypt1to4Keys is idempotent and mutex-protected.
	if err := loadCrypt1to4Keys(); err != nil {
		t.Fatalf("loadCrypt1to4Keys: %v", err)
	}

	expected := []struct {
		name     string
		wantSize int // key.Size() in bytes
	}{
		{"crypt", 128},  // RSA-1024
		{"crypt2", 512}, // RSA-4096
		{"crypt3", 512}, // RSA-4096
		{"crypt4", 512}, // RSA-4096
	}

	crypt1to4Mu.Lock()
	defer crypt1to4Mu.Unlock()

	if len(crypt1to4Keys) != len(expected) {
		t.Errorf("loaded %d keys, want %d", len(crypt1to4Keys), len(expected))
	}

	for _, e := range expected {
		key, ok := crypt1to4Keys[e.name]
		if !ok {
			t.Errorf("missing key %q", e.name)
			continue
		}
		if key == nil {
			t.Errorf("key %q is nil", e.name)
			continue
		}
		if got := key.Size(); got != e.wantSize {
			t.Errorf("key %q Size() = %d, want %d", e.name, got, e.wantSize)
		}
	}
}

func TestDecryptCrypt1to4_InvalidKeyName(t *testing.T) {
	t.Parallel()
	_, err := DecryptCrypt1to4("nonexistent", "AAA")
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "unknown key") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptCrypt1to4_InvalidBase64(t *testing.T) {
	t.Parallel()
	_, err := DecryptCrypt1to4("crypt", "!!!invalid-b64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "decoding payload") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecryptCrypt1to4_WrongKeySize(t *testing.T) {
	t.Parallel()
	// "AAAA" decodes to 3 bytes; RSA-1024 key size is 128 → 3 % 128 != 0
	_, err := DecryptCrypt1to4("crypt", "AAAA")
	if err == nil {
		t.Fatal("expected error for wrong key size")
	}
	if !strings.Contains(err.Error(), "invalid payload length") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDecrypt_AllVersions(t *testing.T) {
	t.Parallel()
	d, err := NewDecryptorEmbedded()
	if err != nil {
		t.Fatalf("NewDecryptorEmbedded: %v", err)
	}

	tests := []struct {
		name    string
		link    string
		wantErr string // substring the error must contain
	}{
		{"crypt5 short payload", "happ://crypt5/AA", "too short"},
		{"crypt2 invalid length", "happ://crypt2/AA", "invalid payload length"},
		{"crypt1 invalid length", "happ://crypt/AA", "invalid payload length"},
		{"unsupported version", "happ://crypt99/AA", "unsupported crypt version"},
		{"bare happ://", "happ://", "too short"},
		{"plain fallback", "plain", "too short"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := d.Decrypt(tc.link)
			if err == nil {
				t.Fatalf("Decrypt(%q): expected error", tc.link)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Decrypt(%q) error = %v, want substring %q", tc.link, err, tc.wantErr)
			}
		})
	}
}
