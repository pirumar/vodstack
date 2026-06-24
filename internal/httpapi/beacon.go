package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/metrics"
)

// beaconRequest is the player-side QoE payload.
type beaconRequest struct {
	VideoID    string   `json:"videoId"`
	LibraryID  string   `json:"libraryId"`
	SessionID  string   `json:"sessionId"`
	// VisitorID is the persistent per-browser id (localStorage) used to count
	// unique viewers across reloads. Optional: older players and storage-blocked
	// browsers omit it, and analytics falls back to session_id for those rows.
	VisitorID  string   `json:"visitorId,omitempty"`
	Event      string   `json:"event"` // start|playing|rebuffer|error|progress|ended
	Position   *float64 `json:"position,omitempty"`
	Value      *float64 `json:"value,omitempty"` // startup ms / watch % / etc.
	Bitrate    *int     `json:"bitrate,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	// VT is the optional signed viewer token. When present and valid it
	// attributes this event to a stable end-user for resume / watch history.
	VT string `json:"vt,omitempty"`
}

// handleBeacon ingests one viewer QoE event. Public (called from the player,
// possibly inside a third-party iframe), CORS-open, IP-rate-limited, and
// fire-and-forget: it records the event in playback_events and bumps Prometheus
// counters, then returns 204 regardless of downstream hiccups.
func (s *Server) handleBeacon(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Cheap per-IP throttle so a misbehaving player can't flood us.
	if s.limiter != nil && !s.limiter.limiterFor("beacon:"+clientIP(r)).Allow() {
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	var req beaconRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if req.VideoID == "" || req.LibraryID == "" || req.SessionID == "" || req.Event == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	val := 0.0
	if req.Value != nil {
		val = *req.Value
	}
	metrics.ObservePlayback(req.Event, val)

	_ = s.db.InsertPlaybackEvent(r.Context(), db.PlaybackEvent{
		VideoID:    req.VideoID,
		LibraryID:  req.LibraryID,
		SessionID:  req.SessionID,
		VisitorID:  req.VisitorID,
		EventType:  req.Event,
		Position:   req.Position,
		Value:      req.Value,
		Bitrate:    req.Bitrate,
		Resolution: req.Resolution,
		Country:    clientCountry(r),
		Device:     deviceClass(r.UserAgent()),
	})

	// If a valid viewer token rode along, roll the event up into per-viewer
	// progress (resume point + watch history). Best-effort and fire-and-forget:
	// anonymous viewers (no/expired vt) just skip this, the analytics event above
	// is still recorded either way.
	if req.VT != "" && req.Position != nil {
		if viewerID, ok := s.signer.VerifyViewer(req.LibraryID, req.VT); ok {
			_ = s.db.UpsertViewerProgress(r.Context(), db.ViewerProgressUpdate{
				LibraryID: req.LibraryID,
				ViewerID:  viewerID,
				VideoID:   req.VideoID,
				Position:  *req.Position,
				Event:     req.Event,
			})
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func clientIP(r *http.Request) string {
	if r.RemoteAddr == "" {
		return "?"
	}
	if i := strings.LastIndexByte(r.RemoteAddr, ':'); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// clientCountry resolves the viewer's ISO country from the CDN edge. Traffic
// reaches us through Cloudflare (cloudflared tunnel), which sets CF-IPCountry;
// we fall back to a couple of common edge headers. Returns "" when unknown or a
// non-geographic placeholder ("XX" unknown, "T1" Tor), so it lands as NULL.
func clientCountry(r *http.Request) string {
	cc := r.Header.Get("CF-IPCountry")
	if cc == "" {
		cc = r.Header.Get("X-Country-Code")
	}
	if cc == "" {
		cc = r.Header.Get("X-Geo-Country")
	}
	cc = strings.ToUpper(strings.TrimSpace(cc))
	if len(cc) != 2 || cc == "XX" || cc == "T1" {
		return ""
	}
	return cc
}

// deviceClass is a coarse UA bucket (desktop/mobile/tv) for breakdowns.
func deviceClass(ua string) string {
	u := strings.ToLower(ua)
	switch {
	case strings.Contains(u, "smart-tv"), strings.Contains(u, "smarttv"),
		strings.Contains(u, "appletv"), strings.Contains(u, "crkey"),
		strings.Contains(u, "roku"):
		return "tv"
	case strings.Contains(u, "mobile"), strings.Contains(u, "android"),
		strings.Contains(u, "iphone"), strings.Contains(u, "ipad"):
		return "mobile"
	case ua == "":
		return ""
	default:
		return "desktop"
	}
}
