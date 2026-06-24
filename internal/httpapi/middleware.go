package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/pirumar/vodstack/internal/auth"
	"github.com/pirumar/vodstack/internal/db"
)

type ctxKey string

const (
	ctxLibraryID ctxKey = "libraryID"
	ctxRateKey   ctxKey = "rateKey"
)

// libraryAuth validates the API key carried as "Authorization: Bearer <key>"
// (or "AccessKey: <key>"). The {libraryId} path segment must exist. A key is
// accepted if it matches a non-revoked row in api_keys for that library, or —
// for backward compatibility with the dev seed — the library's own
// api_key_hash. The resolved credential is stashed for downstream rate limiting.
func (s *Server) libraryAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		libraryID := chi.URLParam(r, "libraryId")
		if libraryID == "" {
			writeError(w, http.StatusBadRequest, "missing library id")
			return
		}

		key := bearerToken(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "missing api key")
			return
		}

		lib, err := s.db.GetLibrary(r.Context(), libraryID)
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unknown library")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "auth lookup failed")
			return
		}

		keyHash := auth.HashAPIKey(key)
		rateKey := "lib:" + libraryID // default principal for rate limiting

		// Prefer a real api_keys row; fall back to the library seed key.
		k, err := s.db.LookupAPIKey(r.Context(), libraryID, keyHash)
		switch {
		case err == nil:
			rateKey = "key:" + k.ID
			go s.touchKey(k.ID) // best-effort, off the request path
		case errors.Is(err, db.ErrNotFound):
			if lib.APIKeyHash == "" || !auth.Equal(keyHash, lib.APIKeyHash) {
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
		default:
			writeError(w, http.StatusInternalServerError, "auth lookup failed")
			return
		}

		ctx := context.WithValue(r.Context(), ctxLibraryID, libraryID)
		ctx = context.WithValue(ctx, ctxRateKey, rateKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// touchKey records last-use without blocking the request. Uses a short
// independent context since the request context is already being served.
func (s *Server) touchKey(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.db.TouchAPIKey(ctx, id)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		// Fallback header to mirror Bunny's AccessKey style.
		return r.Header.Get("AccessKey")
	}
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return strings.TrimSpace(h)
}

func libraryFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxLibraryID).(string)
	return v
}

// rateKeyFromCtx returns the credential principal resolved by libraryAuth, used
// as the rate-limit bucket key. Falls back to the library id.
func rateKeyFromCtx(ctx context.Context) string {
	if v, _ := ctx.Value(ctxRateKey).(string); v != "" {
		return v
	}
	return "lib:" + libraryFromCtx(ctx)
}
