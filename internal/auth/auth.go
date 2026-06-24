// Package auth handles library API-key hashing and constant-time comparison.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// HashAPIKey returns the hex SHA-256 of an API key. Keys are stored hashed; the
// plaintext is only ever held by the caller (the integrating app/admin).
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Equal compares two hashes in constant time.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
