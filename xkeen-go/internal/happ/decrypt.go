// Package happ decrypts happ://crypt5/ deep links and converts sing-box
// subscription responses to xkeen ProxyEntry values.
//
// Algorithm reference: github.com/LeeeeT/happ-decryptor.
// crypt5 uses RSA-4096 (PKCS#1 v1.5) → swapAdjacent → base64 → XOR with
// salt → ChaCha20-Poly1305 to protect the subscription URL.
package happ

import (
	"crypto/rsa"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// embeddedKeys holds the 36 RSA-4096 private keys indexed by 8-char marker.
// The keys are embedded at compile time so the binary is self-contained.
//
//go:embed assets/crypt5-keys.json
var embeddedKeys []byte

// swapBlockHalves swaps the two halves of each 4-byte block: ABCD → CDAB.
// This operation is its own inverse.
func swapBlockHalves(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	full := len(out) - (len(out) % 4)
	for i := 0; i < full; i += 4 {
		out[i], out[i+2] = out[i+2], out[i]
		out[i+1], out[i+3] = out[i+3], out[i+1]
	}
	return out
}

// swapAdjacent swaps every pair of adjacent bytes: [A,B,C,D] → [B,A,D,C].
// This operation is its own inverse.
func swapAdjacent(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	for i := 0; i+1 < len(out); i += 2 {
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

// b64Decode decodes URL-safe base64 with explicit padding fix.
func b64Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	// Add padding
	pad := (4 - len(s)%4) % 4
	s += strings.Repeat("=", pad)
	return base64.StdEncoding.DecodeString(s)
}

// Decryptor holds RSA private keys indexed by marker and caches
// parsed *rsa.PrivateKey values.
type Decryptor struct {
	mu      sync.Mutex
	keyB64s map[string]string // marker → base64 PKCS#8
	cache   map[string]*rsa.PrivateKey
}

// NewDecryptor creates a Decryptor with an explicit key table.
func NewDecryptor(keys map[string]string) (*Decryptor, error) {
	return &Decryptor{
		keyB64s: keys,
		cache:   make(map[string]*rsa.PrivateKey),
	}, nil
}

// NewDecryptorEmbedded creates a Decryptor using the compile-time embedded
// key set. The resulting binary is fully self-contained.
func NewDecryptorEmbedded() (*Decryptor, error) {
	var keys map[string]string
	if err := json.Unmarshal(embeddedKeys, &keys); err != nil {
		return nil, fmt.Errorf("happ: parsing embedded keys: %w", err)
	}
	return NewDecryptor(keys)
}

func (d *Decryptor) getKey(marker string) (*rsa.PrivateKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if pk, ok := d.cache[marker]; ok {
		return pk, nil
	}

	b64, ok := d.keyB64s[marker]
	if !ok {
		return nil, fmt.Errorf("happ: unknown crypt5 marker %q", marker)
	}

	// Build PEM block from base64 string (64-char lines).
	var pemBuf strings.Builder
	pemBuf.WriteString("-----BEGIN PRIVATE KEY-----\n")
	for i := 0; i < len(b64); i += 64 {
		end := i + 64
		if end > len(b64) {
			end = len(b64)
		}
		pemBuf.WriteString(b64[i:end])
		pemBuf.WriteByte('\n')
	}
	pemBuf.WriteString("-----END PRIVATE KEY-----\n")

	block, _ := pem.Decode([]byte(pemBuf.String()))
	if block == nil {
		return nil, fmt.Errorf("happ: failed to decode PEM for marker %q", marker)
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("happ: parsing PKCS#8 key for marker %q: %w", marker, err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("happ: key for marker %q is not RSA", marker)
	}

	d.cache[marker] = rsaKey
	return rsaKey, nil
}

// DecryptCrypt5 decrypts a raw crypt5 payload (everything after
// "crypt5/") and returns the plaintext subscription URL.
func (d *Decryptor) DecryptCrypt5(payload string) (string, error) {
	shuffled := swapBlockHalves([]byte(payload))
	if len(shuffled) < 8 {
		return "", errors.New("happ: crypt5 payload too short")
	}

	// Marker = first 4 bytes + last 4 bytes.
	marker := string(shuffled[:4]) + string(shuffled[len(shuffled)-4:])

	key, err := d.getKey(marker)
	if err != nil {
		return "", err
	}

	body := shuffled[4 : len(shuffled)-4]
	return d.decryptBody(body, key)
}

// Decrypt decrypts a full happ://crypt5/ deep link and returns the
// cleartext subscription URL.
func (d *Decryptor) Decrypt(link string) (string, error) {
	payload := link
	if idx := strings.Index(link, "crypt5/"); idx >= 0 {
		payload = link[idx+7:]
	} else if strings.HasPrefix(link, "crypt5/") {
		payload = link[7:]
	}
	return d.DecryptCrypt5(payload)
}

func (d *Decryptor) decryptBody(body []byte, key *rsa.PrivateKey) (string, error) {
	if len(body) < 13 {
		return "", errors.New("happ: crypt5 body too short")
	}

	nonce := body[:12]

	// Heuristic: if byte 12 is NOT an ASCII digit, treat as salted layout.
	salted := len(body) > 12 && (body[12] < '0' || body[12] > '9')

	var firstErr error
	for _, trySalted := range []bool{salted, !salted} {
		result, err := d.tryDecrypt(body, key, nonce, trySalted)
		if err == nil {
			return result, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return "", firstErr
}

func (d *Decryptor) tryDecrypt(body []byte, key *rsa.PrivateKey, nonce []byte, salted bool) (string, error) {
	start := 12
	var salt []byte

	if salted {
		if len(body) < 22 {
			return "", errors.New("happ: salted header too short")
		}
		salt = body[14:22]
		start = 22
	}

	// Parse ASCII digit segment length.
	end := start
	for end < len(body) && body[end] >= '0' && body[end] <= '9' {
		end++
	}
	if end == start {
		return "", errors.New("happ: segment length missing")
	}

	segLen := 0
	for i := start; i < end; i++ {
		segLen = segLen*10 + int(body[i]-'0')
	}

	packed := body[end:]
	if segLen > len(packed)-1 {
		return "", errors.New("happ: segment truncated")
	}

	// packed[0] is a separator byte; then encryptedURL + RSA ciphertext.
	encryptedURL := string(packed[1 : segLen+1])
	rsaCiphertext, err := b64Decode(string(packed[segLen+1:]))
	if err != nil {
		return "", fmt.Errorf("happ: decoding RSA ciphertext: %w", err)
	}

	// RSA-4096 PKCS#1 v1.5 decrypt.
	rsaPlaintext, err := rsa.DecryptPKCS1v15(nil, key, rsaCiphertext)
	if err != nil {
		return "", fmt.Errorf("happ: RSA decrypt: %w", err)
	}

	// swapAdjacent → base64 → 32-byte ChaCha20 key.
	swapped := swapAdjacent(rsaPlaintext)
	rsaValue, err := b64Decode(string(swapped))
	if err != nil {
		return "", fmt.Errorf("happ: decoding RSA plaintext: %w", err)
	}
	if len(rsaValue) != 32 {
		return "", fmt.Errorf("happ: unexpected key length %d (want 32)", len(rsaValue))
	}

	chachaKey := make([]byte, 32)
	copy(chachaKey, rsaValue)
	if salt != nil {
		for i := range chachaKey {
			chachaKey[i] ^= salt[i%len(salt)]
		}
	}

	// ChaCha20-Poly1305 decrypt the encrypted URL segment.
	ciphertext, err := b64Decode(encryptedURL)
	if err != nil {
		return "", fmt.Errorf("happ: decoding encrypted URL: %w", err)
	}

	cipher, err := chacha20poly1305.New(chachaKey)
	if err != nil {
		return "", fmt.Errorf("happ: creating ChaCha20 cipher: %w", err)
	}

	intermediate, err := cipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("happ: ChaCha20 auth failed: %w", err)
	}

	// Final swapAdjacent + base64.
	swappedIntermediate := swapAdjacent(intermediate)
	plaintext, err := b64Decode(string(swappedIntermediate))
	if err != nil {
		return "", fmt.Errorf("happ: decoding final plaintext: %w", err)
	}

	return string(plaintext), nil
}
