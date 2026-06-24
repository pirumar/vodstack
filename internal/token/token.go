// Package token mints HMAC-SHA256 signed-URL tokens.
//
// One token authorizes a path PREFIX (e.g. "/hls/{videoId}/") until an expiry.
// Because the master playlist, variant playlists, init segments and all media
// segments live under that prefix, a single token covers an entire playback
// session with no per-segment URL rewriting: hls.js carries the same query
// string on every child request.
//
// The signature is computed over "<prefix>\n<exp>". The edge validator (njs)
// recomputes it from the request path + ?exp using the same secret. Keeping the
// scheme this small makes the njs implementation trivial and fast.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signer holds the shared secret.
type Signer struct {
	secret []byte
}

func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret)}
}

// sign computes the base64url(unpadded) HMAC-SHA256 of arbitrary material. Both
// the URL signer and the viewer-token signer go through here so the algorithm
// never drifts between them.
func (s *Signer) sign(material string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(material))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Sign returns the base64url signature for a prefix valid until exp (unix sec).
func (s *Signer) Sign(prefix string, exp int64) string {
	return s.sign(prefix + "\n" + strconv.FormatInt(exp, 10))
}

// Verify checks a signature for a prefix/exp (used in tests; the edge njs is the
// production verifier).
func (s *Signer) Verify(prefix string, exp int64, sig string) bool {
	if time.Now().Unix() > exp {
		return false
	}
	expected := s.Sign(prefix, exp)
	return hmac.Equal([]byte(expected), []byte(sig))
}

// --- Admin session cookies ---
//
// A session value is "<exp>.<sig>" where sig signs the literal subject "admin"
// together with exp. Same HMAC machinery as signed URLs, different subject.

// NewSession mints a session value valid for ttl.
func (s *Signer) NewSession(ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	return strconv.FormatInt(exp, 10) + "." + s.Sign("admin", exp)
}

// ValidSession reports whether a session value is well-formed and unexpired.
func (s *Signer) ValidSession(value string) bool {
	dot := strings.IndexByte(value, '.')
	if dot < 0 {
		return false
	}
	expStr, sig := value[:dot], value[dot+1:]
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return false
	}
	return s.Verify("admin", exp, sig)
}

// --- Viewer tokens ---
//
// A viewer token (the "vt" embed query param) binds a platform-supplied,
// opaque end-user id to a library until an expiry. The platform mints it
// server-to-server (with its API key) and renders the iframe with ?vt=...; the
// player carries it on progress beacons and the resume fetch. vodstack verifies it
// with its own secret, so the platform never holds the signing key and a viewer
// cannot spoof another viewer's id.
//
// Signed material: "vt\n<libraryID>\n<viewerID>\n<exp>".
// Wire form:       base64url(viewerID) + "." + libraryID + "." + exp + "." + sig
//
// viewerID is base64url-encoded because it is platform-opaque and may contain
// arbitrary characters; "." separates the fields. The signature is recomputed
// against the PATH libraryID at verify time, so a token minted for library A
// cannot be replayed on library B.

func viewerMaterial(libraryID, viewerID string, exp int64) string {
	return "vt\n" + libraryID + "\n" + viewerID + "\n" + strconv.FormatInt(exp, 10)
}

// SignViewer mints a viewer token binding viewerID to libraryID until exp.
func (s *Signer) SignViewer(libraryID, viewerID string, exp int64) string {
	sig := s.sign(viewerMaterial(libraryID, viewerID, exp))
	return base64.RawURLEncoding.EncodeToString([]byte(viewerID)) + "." +
		libraryID + "." + strconv.FormatInt(exp, 10) + "." + sig
}

// VerifyViewer parses and verifies a viewer token against the request's
// libraryID, returning the viewer id it authorizes. ok is false on a malformed
// token, a bad signature, an expired token, or a library mismatch.
func (s *Signer) VerifyViewer(libraryID, vt string) (viewerID string, ok bool) {
	parts := strings.Split(vt, ".")
	if len(parts) != 4 {
		return "", false
	}
	vidB64, tokLib, expStr, sig := parts[0], parts[1], parts[2], parts[3]
	if tokLib != libraryID {
		return "", false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(vidB64)
	if err != nil {
		return "", false
	}
	vid := string(raw)
	expected := s.sign(viewerMaterial(libraryID, vid, exp))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", false
	}
	return vid, true
}

// SignedURL builds a fully-signed URL for the given object path under a prefix.
//
//	baseURL = https://cdn.example.com
//	prefix  = /hls/<id>/
//	object  = master.m3u8
//
// -> https://cdn.example.com/hls/<id>/master.m3u8?exp=...&token=...
func (s *Signer) SignedURL(baseURL, prefix, object string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	sig := s.Sign(prefix, exp)
	u := strings.TrimRight(baseURL, "/") + prefix + object
	q := url.Values{}
	q.Set("exp", strconv.FormatInt(exp, 10))
	q.Set("token", sig)
	return u + "?" + q.Encode()
}
