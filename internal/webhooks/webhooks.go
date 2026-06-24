// Package webhooks turns video lifecycle events into signed HTTP POSTs to
// customer-registered endpoints. Dispatch records a delivery row per subscribed
// endpoint and enqueues an asynq task; the worker performs the actual POST and
// retries (see internal/worker). Payloads are HMAC-SHA256 signed with the
// endpoint's own secret so receivers can verify authenticity.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
)

// Event types. Keep these stable: they are part of the public API contract.
const (
	EventVideoCreated  = "video.created"
	EventVideoUploaded = "video.uploaded"
	EventVideoEncoded   = "video.encoded"
	EventVideoAV1Ready  = "video.av1_ready"
	EventVideoCaptioned = "video.captioned"
	EventVideoIndexed   = "video.indexed"
	EventVideoEnriched  = "video.enriched"
	EventVideoEncrypted = "video.encrypted"
	EventVideoFailed    = "video.failed"
	EventVideoDeleted   = "video.deleted"
)

// HTTP headers attached to every delivery.
const (
	HeaderSignature = "X-Vodstack-Signature" // "sha256=<hex>"
	HeaderEvent     = "X-Vodstack-Event"
	HeaderDelivery  = "X-Vodstack-Delivery"
)

// Event is a lifecycle event to dispatch.
type Event struct {
	Type      string
	LibraryID string
	VideoID   string
	Data      map[string]any // optional extra fields merged into the payload
}

// envelope is the JSON body POSTed to endpoints.
type envelope struct {
	Event     string         `json:"event"`
	LibraryID string         `json:"libraryId"`
	VideoID   string         `json:"videoId,omitempty"`
	Timestamp string         `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// Sign returns the hex HMAC-SHA256 of body under secret, prefixed "sha256=".
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Dispatcher fans an event out to a library's subscribed endpoints.
type Dispatcher struct {
	db    *db.DB
	queue *queue.Client
}

func NewDispatcher(database *db.DB, q *queue.Client) *Dispatcher {
	return &Dispatcher{db: database, queue: q}
}

// Dispatch records and enqueues a delivery for every active endpoint subscribed
// to the event. It is best-effort and non-blocking on the hot path: errors are
// logged, never returned to the triggering request. A nil Dispatcher is a no-op
// so callers need not nil-check.
func (d *Dispatcher) Dispatch(ctx context.Context, ev Event) {
	if d == nil {
		return
	}
	endpoints, err := d.db.ActiveEndpointsForEvent(ctx, ev.LibraryID, ev.Type)
	if err != nil {
		log.Printf("webhooks: list endpoints (%s/%s): %v", ev.LibraryID, ev.Type, err)
		return
	}
	if len(endpoints) == 0 {
		return
	}

	body, err := json.Marshal(envelope{
		Event:     ev.Type,
		LibraryID: ev.LibraryID,
		VideoID:   ev.VideoID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data:      ev.Data,
	})
	if err != nil {
		log.Printf("webhooks: marshal payload (%s): %v", ev.Type, err)
		return
	}

	for _, e := range endpoints {
		deliveryID := uuid.NewString()
		if err := d.db.CreateWebhookDelivery(ctx, deliveryID, e.ID, ev.Type, body); err != nil {
			log.Printf("webhooks: create delivery (endpoint %s): %v", e.ID, err)
			continue
		}
		if err := d.queue.EnqueueWebhook(deliveryID); err != nil {
			log.Printf("webhooks: enqueue delivery %s: %v", deliveryID, err)
		}
	}
}
