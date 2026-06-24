package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// WebhookEndpoint is a customer-registered URL that receives event POSTs.
type WebhookEndpoint struct {
	ID        string    `json:"id"`
	LibraryID string    `json:"libraryId"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"` // never serialized back to clients after creation
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"createdAt"`
}

func (d *DB) CreateWebhookEndpoint(ctx context.Context, id, libraryID, url, secret string, events []string) error {
	if events == nil {
		events = []string{}
	}
	_, err := d.pool.Exec(ctx, `
		INSERT INTO webhook_endpoints (id, library_id, url, secret, events)
		VALUES ($1, $2, $3, $4, $5)`,
		id, libraryID, url, secret, events)
	return err
}

func (d *DB) ListWebhookEndpoints(ctx context.Context, libraryID string) ([]WebhookEndpoint, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, library_id, url, secret, events, active, created_at
		FROM webhook_endpoints WHERE library_id=$1 ORDER BY created_at DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookEndpoint
	for rows.Next() {
		var e WebhookEndpoint
		if err := rows.Scan(&e.ID, &e.LibraryID, &e.URL, &e.Secret, &e.Events,
			&e.Active, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) DeleteWebhookEndpoint(ctx context.Context, libraryID, id string) error {
	tag, err := d.pool.Exec(ctx,
		`DELETE FROM webhook_endpoints WHERE id=$1 AND library_id=$2`, id, libraryID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveEndpointsForEvent returns the active endpoints in a library that are
// subscribed to eventType (an empty events array means "all events").
func (d *DB) ActiveEndpointsForEvent(ctx context.Context, libraryID, eventType string) ([]WebhookEndpoint, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, library_id, url, secret, events, active, created_at
		FROM webhook_endpoints
		WHERE library_id=$1 AND active = TRUE
		  AND (cardinality(events) = 0 OR $2 = ANY(events))`,
		libraryID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebhookEndpoint
	for rows.Next() {
		var e WebhookEndpoint
		if err := rows.Scan(&e.ID, &e.LibraryID, &e.URL, &e.Secret, &e.Events,
			&e.Active, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateWebhookDelivery records a pending delivery. The asynq task carries the
// returned id and updates this row as it (re)tries.
func (d *DB) CreateWebhookDelivery(ctx context.Context, id, endpointID, eventType string, payload []byte) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO webhook_deliveries (id, endpoint_id, event_type, payload)
		VALUES ($1, $2, $3, $4)`,
		id, endpointID, eventType, payload)
	return err
}

// WebhookSend is everything the delivery worker needs to POST one delivery.
type WebhookSend struct {
	DeliveryID string
	URL        string
	Secret     string
	EventType  string
	Payload    []byte
	Attempts   int
}

// GetWebhookSend joins a delivery to its endpoint for sending.
func (d *DB) GetWebhookSend(ctx context.Context, deliveryID string) (*WebhookSend, error) {
	var s WebhookSend
	s.DeliveryID = deliveryID
	err := d.pool.QueryRow(ctx, `
		SELECT e.url, e.secret, d.event_type, d.payload, d.attempts
		FROM webhook_deliveries d
		JOIN webhook_endpoints e ON e.id = d.endpoint_id
		WHERE d.id=$1`, deliveryID,
	).Scan(&s.URL, &s.Secret, &s.EventType, &s.Payload, &s.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// MarkWebhookDelivered records a successful delivery.
func (d *DB) MarkWebhookDelivered(ctx context.Context, deliveryID string, code int) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status='delivered', attempts=attempts+1, response_code=$2,
		    last_error=NULL, updated_at=now()
		WHERE id=$1`, deliveryID, code)
	return err
}

// MarkWebhookFailed records a failed attempt (code 0 if no response).
func (d *DB) MarkWebhookFailed(ctx context.Context, deliveryID string, code int, errMsg string) error {
	var codePtr *int
	if code != 0 {
		codePtr = &code
	}
	_, err := d.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status='failed', attempts=attempts+1, response_code=$2,
		    last_error=$3, updated_at=now()
		WHERE id=$1`, deliveryID, codePtr, errMsg)
	return err
}
