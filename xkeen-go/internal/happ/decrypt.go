// Package happ decrypts happ://cryptX/ deep links and converts sing-box
// subscription responses to xkeen ProxyEntry values.
//
// Algorithm reference: github.com/LeeeeT/happ-decryptor.
//
// Supported versions:
//   crypt (v1) — RSA-1024 PKCS#1 v1.5, direct
//   crypt2    — RSA-4096 PKCS#1 v1.5, direct
//   crypt3    — RSA-4096 PKCS#1 v1.5, direct
//   crypt4    — RSA-4096 PKCS#1 v1.5, direct
//   crypt5    — RSA-4096 PKCS#1 v1.5 + swapBlockHalves + swapAdjacent +
//               XOR with salt + ChaCha20-Poly1305
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

// embeddedKeys holds the 36 RSA-4096 private keys for crypt5, indexed by
// 8-char marker. Embedded at compile time so the binary is self-contained.
//
//go:embed assets/crypt5-keys.json
var embeddedKeys []byte

// embeddedCrypt1to4Keys holds the 4 PKCS#1 RSA private keys for crypt1–4.
//
//go:embed assets/crypt-keys.json
var embeddedCrypt1to4Keys []byte

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

// ---------- crypt1-4 (direct RSA PKCS#1 v1.5, no ChaCha20) ----------

var (
	crypt1to4Mu     sync.Mutex
	crypt1to4Loaded bool
	crypt1to4Keys   map[string]*rsa.PrivateKey
)

// parsePKCS1Key parses a PKCS#1 base64-encoded RSA private key.
func parsePKCS1Key(b64 string) (*rsa.PrivateKey, error) {
	// Rebuild PEM with 64-char lines.
	var buf strings.Builder
	buf.WriteString("-----BEGIN RSA PRIVATE KEY-----\n")
	for i := 0; i < len(b64); i += 64 {
		end := i + 64
		if end > len(b64) {
			end = len(b64)
		}
		buf.WriteString(b64[i:end])
		buf.WriteByte('\n')
	}
	buf.WriteString("-----END RSA PRIVATE KEY-----\n")

	block, _ := pem.Decode([]byte(buf.String()))
	if block == nil {
		return nil, fmt.Errorf("happ: failed to decode PKCS#1 PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("happ: parsing PKCS#1 key: %w", err)
	}
	return key, nil
}

// loadCrypt1to4Keys loads and parses the 4 PKCS#1 RSA keys for crypt1-4.
func loadCrypt1to4Keys() error {
	crypt1to4Mu.Lock()
	defer crypt1to4Mu.Unlock()

	if crypt1to4Loaded {
		return nil
	}

	var rawKeys map[string]string
	if err := json.Unmarshal(embeddedCrypt1to4Keys, &rawKeys); err != nil {
		return fmt.Errorf("happ: parsing crypt1-4 keys: %w", err)
	}

	crypt1to4Keys = make(map[string]*rsa.PrivateKey, len(rawKeys))
	for name, b64 := range rawKeys {
		key, err := parsePKCS1Key(b64)
		if err != nil {
			return fmt.Errorf("happ: loading key %q: %w", name, err)
		}
		crypt1to4Keys[name] = key
	}

	crypt1to4Loaded = true
	return nil
}

// DecryptCrypt1to4 decrypts a crypt1-4 payload using direct RSA PKCS#1 v1.5.
// The payload is URL-safe base64 encoded RSA ciphertext, split into
// key-size chunks (128 bytes for RSA-1024, 512 bytes for RSA-4096).
func DecryptCrypt1to4(keyName, payload string) (string, error) {
	if err := loadCrypt1to4Keys(); err != nil {
		return "", err
	}

	crypt1to4Mu.Lock()
	privateKey := crypt1to4Keys[keyName]
	crypt1to4Mu.Unlock()

	if privateKey == nil {
		return "", fmt.Errorf("happ: unknown key %q", keyName)
	}

	cipherBytes, err := b64Decode(payload)
	if err != nil {
		return "", fmt.Errorf("happ: decoding payload: %w", err)
	}

	keySize := privateKey.Size() // 128 for RSA-1024, 512 for RSA-4096
	if len(cipherBytes) == 0 || len(cipherBytes)%keySize != 0 {
		return "", fmt.Errorf("happ: invalid payload length %d (key size %d)", len(cipherBytes), keySize)
	}

	var result []byte
	for i := 0; i < len(cipherBytes); i += keySize {
		chunk, err := rsa.DecryptPKCS1v15(nil, privateKey, cipherBytes[i:i+keySize])
		if err != nil {
			return "", fmt.Errorf("happ: RSA decrypt %q: %w", keyName, err)
		}
		result = append(result, chunk...)
	}

	return string(result), nil
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

// Decrypt decrypts a full happ://cryptX/ deep link and returns the
// cleartext subscription URL.
func (d *Decryptor) Decrypt(link string) (string, error) {
	// Strip optional happ:// scheme prefix (happ://crypt5/... → crypt5/...).
	payload := strings.TrimPrefix(link, "happ://")

	// Extract version prefix (crypt5/...).
	const prefix = "crypt"
	slash := strings.IndexByte(payload, '/')
	if slash < 0 || !strings.HasPrefix(payload, prefix) {
		// Bare payload without cryptX/ prefix — try crypt5 for backward compat.
		return d.DecryptCrypt5(payload)
	}

	version := payload[len(prefix):slash]
	rest := payload[slash+1:]

	switch version {
	case "5":
		return d.DecryptCrypt5(rest)
	case "", "2", "3", "4":
		// crypt (v1, no number) and crypt2-4 use RSA PKCS#1 direct decrypt.
		// The key name is "crypt" + version (or just "crypt" for v1).
		keyName := prefix + version
		return DecryptCrypt1to4(keyName, rest)
	default:
		return "", fmt.Errorf("happ: unsupported crypt version %q (supported: crypt, crypt2-5)", version)
	}
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
