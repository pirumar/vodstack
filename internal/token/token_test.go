package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	s := NewSigner("secret-key")
	prefix := "/hls/abc-123/"
	exp := time.Now().Add(time.Hour).Unix()

	sig := s.Sign(prefix, exp)
	if !s.Verify(prefix, exp, sig) {
		t.Fatal("valid signature failed to verify")
	}
	if s.Verify(prefix, exp, sig+"x") {
		t.Fatal("tampered signature verified")
	}
	if s.Verify("/hls/other/", exp, sig) {
		t.Fatal("signature accepted for a different prefix")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	s := NewSigner("secret-key")
	prefix := "/hls/abc/"
	exp := time.Now().Add(-time.Second).Unix()
	if s.Verify(prefix, exp, s.Sign(prefix, exp)) {
		t.Fatal("expired token verified")
	}
}

// TestSignAlgorithmParity pins the exact algorithm the edge njs validator must
// reproduce: base64url(unpadded) of HMAC_SHA256(secret, prefix+"\n"+exp).
// If this changes, deploy/nginx/njs/hmac_auth.js must change too.
func TestSignAlgorithmParity(t *testing.T) {
	secret := "dev-secret"
	prefix := "/hls/v1/"
	exp := int64(1781370000)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(prefix + "\n" + "1781370000"))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	got := NewSigner(secret).Sign(prefix, exp)
	if got != want {
		t.Fatalf("signature algorithm drifted: got %q want %q", got, want)
	}
}

func TestSignedURL(t *testing.T) {
	s := NewSigner("k")
	raw := s.SignedURL("https://cdn.example.com", "/hls/v1/", "master.m3u8", time.Hour)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("bad url: %v", err)
	}
	if u.Path != "/hls/v1/master.m3u8" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	if q.Get("exp") == "" || q.Get("token") == "" {
		t.Errorf("missing exp/token in %q", raw)
	}
	if strings.Contains(q.Get("token"), "=") {
		t.Errorf("token should be unpadded base64url: %q", q.Get("token"))
	}
}

func TestSignVerifyViewerRoundTrip(t *testing.T) {
	s := NewSigner("secret-key")
	lib, viewer := "lib-1", "user-42"
	exp := time.Now().Add(time.Hour).Unix()

	vt := s.SignViewer(lib, viewer, exp)
	got, ok := s.VerifyViewer(lib, vt)
	if !ok || got != viewer {
		t.Fatalf("round-trip failed: ok=%v got=%q want=%q", ok, got, viewer)
	}

	// Tampered signature.
	if _, ok := s.VerifyViewer(lib, vt+"x"); ok {
		t.Fatal("tampered viewer token verified")
	}
	// Wrong library (cross-tenant replay).
	if _, ok := s.VerifyViewer("lib-2", vt); ok {
		t.Fatal("viewer token accepted for a different library")
	}
	// Different secret.
	if _, ok := NewSigner("other").VerifyViewer(lib, vt); ok {
		t.Fatal("viewer token accepted under a different secret")
	}
	// Malformed.
	if _, ok := s.VerifyViewer(lib, "garbage"); ok {
		t.Fatal("garbage viewer token accepted")
	}
}

func TestVerifyViewerRejectsExpired(t *testing.T) {
	s := NewSigner("secret-key")
	exp := time.Now().Add(-time.Second).Unix()
	vt := s.SignViewer("lib-1", "user-1", exp)
	if _, ok := s.VerifyViewer("lib-1", vt); ok {
		t.Fatal("expired viewer token verified")
	}
}

func TestSignViewerSpecialCharacters(t *testing.T) {
	s := NewSigner("secret-key")
	// Platform ids can be arbitrary (emails, uuids with dots, etc.).
	for _, viewer := range []string{"a.b.c", "user@example.com", "u/1+2=3", "日本語"} {
		exp := time.Now().Add(time.Hour).Unix()
		vt := s.SignViewer("lib", viewer, exp)
		got, ok := s.VerifyViewer("lib", vt)
		if !ok || got != viewer {
			t.Fatalf("special id %q round-trip failed: ok=%v got=%q", viewer, ok, got)
		}
	}
}

func TestSession(t *testing.T) {
	s := NewSigner("k")
	val := s.NewSession(time.Hour)
	if !s.ValidSession(val) {
		t.Fatal("fresh session rejected")
	}
	if s.ValidSession(val + "x") {
		t.Fatal("tampered session accepted")
	}
	if s.ValidSession("garbage") {
		t.Fatal("garbage session accepted")
	}
	if NewSigner("other").ValidSession(val) {
		t.Fatal("session accepted under a different secret")
	}
}
