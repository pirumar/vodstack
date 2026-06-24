package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSign(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"event":"video.encoded","videoId":"abc"}`)

	got := Sign(secret, body)

	if !strings.HasPrefix(got, "sha256=") {
		t.Fatalf("signature missing sha256= prefix: %q", got)
	}

	// Recompute independently (what a receiver does) and compare.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature mismatch:\n got %s\nwant %s", got, want)
	}
}

func TestSignDiffersBySecret(t *testing.T) {
	body := []byte("payload")
	if Sign("secretA", body) == Sign("secretB", body) {
		t.Fatal("signatures with different secrets must differ")
	}
}

func TestSignStableForSameInput(t *testing.T) {
	body := []byte("payload")
	if Sign("s", body) != Sign("s", body) {
		t.Fatal("signing must be deterministic")
	}
}
