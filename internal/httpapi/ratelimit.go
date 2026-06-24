package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// rateLimiter is an in-memory per-credential token-bucket registry. Keyed by the
// principal resolved in libraryAuth (api-key id, or library id for the seed
// key). Sufficient for a single API instance; a multi-instance deployment would
// move this to Redis (already present for asynq).
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*entry
	r       rate.Limit
	burst   int
}

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*entry),
		r:       rate.Limit(rps),
		burst:   burst,
	}
	go rl.reap()
	return rl
}

func (rl *rateLimiter) limiterFor(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e, ok := rl.buckets[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(rl.r, rl.burst)}
		rl.buckets[key] = e
	}
	e.seen = time.Now()
	return e.lim
}

// reap drops idle buckets so the map does not grow unbounded over the process
// lifetime.
func (rl *rateLimiter) reap() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-30 * time.Minute)
		rl.mu.Lock()
		for k, e := range rl.buckets {
			if e.seen.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// rateLimit is middleware mounted AFTER libraryAuth so the resolved credential
// is the bucket key. Rejects with 429 + Retry-After when the bucket is empty.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		if !s.limiter.limiterFor(rateKeyFromCtx(r.Context())).Allow() {
			w.Header().Set("Retry-After", strconv.Itoa(1))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
