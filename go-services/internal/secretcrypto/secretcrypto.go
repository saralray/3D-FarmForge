// Package secretcrypto encrypts/decrypts printer connection secrets at rest with
// AES-256-GCM, sharing the PRINTER_SECRET_KEY env var and the
// enc:v1:<iv>:<ciphertext>:<tag> (all base64) wire format with the Node services
// (server/secretCrypto.js) and the former Python poller — a value written by any
// side decrypts on the others. When PRINTER_SECRET_KEY is unset, encryption is
// disabled and values pass through as plaintext.
package secretcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
)

const encPrefix = "enc:v1:"

var hex64 = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// Cipher holds the loaded key (nil when encryption is disabled).
type Cipher struct {
	key []byte
}

// FromEnv loads the key from PRINTER_SECRET_KEY using the same precedence as the
// other services: a 64-char hex string or a base64-encoded 32 bytes is used
// directly; any other non-empty value is sha256'd to 32 bytes; empty disables
// encryption (Cipher with a nil key — pass-through).
func FromEnv() *Cipher {
	raw := strings.TrimSpace(os.Getenv("PRINTER_SECRET_KEY"))
	if raw == "" {
		return &Cipher{key: nil}
	}
	if hex64.MatchString(raw) {
		if b, err := hex.DecodeString(raw); err == nil {
			return &Cipher{key: b}
		}
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return &Cipher{key: b}
	}
	sum := sha256.Sum256([]byte(raw))
	return &Cipher{key: sum[:]}
}

// Enabled reports whether a key is configured.
func (c *Cipher) Enabled() bool { return c != nil && c.key != nil }

// Encrypt returns the enc:v1:... envelope for a plaintext secret. Empty strings,
// already-encrypted values, and the no-key case pass through unchanged.
func (c *Cipher) Encrypt(plain string) string {
	if plain == "" || !c.Enabled() || strings.HasPrefix(plain, encPrefix) {
		return plain
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return plain
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return plain
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return plain
	}
	// Seal returns ciphertext||tag (16-byte tag last); split to match the Node
	// layout, which stores ciphertext and tag separately.
	combined := gcm.Seal(nil, iv, []byte(plain), nil)
	ct, tag := combined[:len(combined)-16], combined[len(combined)-16:]
	enc := base64.StdEncoding
	return encPrefix + enc.EncodeToString(iv) + ":" + enc.EncodeToString(ct) + ":" + enc.EncodeToString(tag)
}

// Decrypt reverses Encrypt. A plaintext (non-enveloped) value is returned as-is;
// an enveloped value with no key, or that fails to decrypt, returns "".
func (c *Cipher) Decrypt(stored string) string {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored // plaintext, or encryption disabled
	}
	if !c.Enabled() {
		return ""
	}
	parts := strings.Split(stored[len(encPrefix):], ":")
	if len(parts) != 3 {
		return ""
	}
	enc := base64.StdEncoding
	iv, err1 := enc.DecodeString(parts[0])
	ct, err2 := enc.DecodeString(parts[1])
	tag, err3 := enc.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return ""
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	plain, err := gcm.Open(nil, iv, append(ct, tag...), nil)
	if err != nil {
		return ""
	}
	return string(plain)
}
