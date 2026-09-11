// Package identity provides cryptographic identity generation.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

// RuntimeID generates a cryptographically random UUID for a new runtime.
func RuntimeID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate runtime ID: %w", err)
	}
	// Set UUID version 4
	b[6] = (b[6] & 0x0f) | 0x40
	// Set variant
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// SessionAPIKey generates a random hex-encoded session API key.
func SessionAPIKey() (string, error) {
	return randomHex(32)
}

// RuntimeSecretKey generates a random hex-encoded runtime secret key.
func RuntimeSecretKey() (string, error) {
	return randomHex(32)
}

// ClaimName derives a Kubernetes-safe claim name from a runtime UUID.
// Format: oh-<first 20 chars of normalized UUID>
func ClaimName(runtimeID string) string {
	normalized := strings.ReplaceAll(runtimeID, "-", "")
	if len(normalized) > 20 {
		normalized = normalized[:20]
	}
	return "oh-" + normalized
}

// SessionHash computes a truncated SHA-256 hash of a session ID suitable
// for use as a Kubernetes label value.
func SessionHash(sessionID string) string {
	h := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(h[:16]) // 32 hex chars
}

// ConstantTimeCompare performs a constant-time string comparison.
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
