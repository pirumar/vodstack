// Package metrics exposes Prometheus instrumentation shared by the API and the
// worker: HTTP request rate/latency and transcode job counts/durations.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vodstack_http_requests_total",
		Help: "HTTP requests by method, route and status.",
	}, []string{"method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "vodstack_http_request_duration_seconds",
		Help:    "HTTP request latency by route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})

	transcodeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vodstack_transcode_total",
		Help: "Transcode jobs by result (success/failed).",
	}, []string{"result"})

	transcodeDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "vodstack_transcode_duration_seconds",
		Help:    "Wall-clock duration of completed transcode jobs.",
		Buckets: []float64{5, 15, 30, 60, 120, 300, 600, 1800, 3600},
	})

	// --- Player QoE (fed by the /beacon endpoint) ---

	playbackEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "vodstack_playback_events_total",
		Help: "Viewer playback events by type (start/playing/rebuffer/error/ended).",
	}, []string{"event"})

	playbackStartup = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "vodstack_playback_startup_seconds",
		Help:    "Time-to-first-frame reported by players.",
		Buckets: []float64{0.25, 0.5, 1, 2, 3, 5, 8, 13},
	})
)

// ObservePlayback records a viewer QoE event. value carries the startup time in
// milliseconds for "start" events (ignored otherwise).
func ObservePlayback(event string, value float64) {
	playbackEvents.WithLabelValues(event).Inc()
	if event == "start" && value > 0 {
		playbackStartup.Observe(value / 1000.0)
	}
}

// Handler serves the Prometheus exposition endpoint.
func Handler() http.Handler { return promhttp.Handler() }

// Middleware records request count + latency. Uses the chi route pattern as the
// label to keep cardinality bounded.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(ww.status)).Inc()
		httpDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// ObserveTranscode records the outcome and duration of a transcode job.
func ObserveTranscode(success bool, d time.Duration) {
	result := "success"
	if !success {
		result = "failed"
	}
	transcodeTotal.WithLabelValues(result).Inc()
	if success {
		transcodeDuration.Observe(d.Seconds())
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying ResponseWriter so http.ResponseController
// (used by e.g. tusd to set read/write deadlines) can reach it.
func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
