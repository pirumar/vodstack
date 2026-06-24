package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hibiken/asynq"

	"github.com/pirumar/vodstack/internal/db"
	"github.com/pirumar/vodstack/internal/queue"
	"github.com/pirumar/vodstack/internal/webhooks"
)

// webhookClient bounds each delivery attempt; asynq handles backoff between
// retries.
var webhookClient = &http.Client{Timeout: 15 * time.Second}

// handleWebhookDeliver loads a recorded delivery, signs it with the endpoint's
// secret, and POSTs it. A non-2xx (or transport error) returns an error so asynq
// retries with exponential backoff up to MaxRetry; the delivery row tracks the
// latest attempt/result.
func (wk *Worker) handleWebhookDeliver(ctx context.Context, t *asynq.Task) error {
	var p queue.WebhookDeliverPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("bad webhook payload: %v: %w", err, asynq.SkipRetry)
	}

	send, err := wk.db.GetWebhookSend(ctx, p.DeliveryID)
	if errors.Is(err, db.ErrNotFound) {
		// Endpoint or delivery was deleted; nothing to retry.
		return fmt.Errorf("webhook delivery %s gone: %w", p.DeliveryID, asynq.SkipRetry)
	}
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, send.URL, bytes.NewReader(send.Payload))
	if err != nil {
		_ = wk.db.MarkWebhookFailed(ctx, send.DeliveryID, 0, err.Error())
		return fmt.Errorf("build webhook request: %v: %w", err, asynq.SkipRetry)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "vodstack-webhooks/1")
	req.Header.Set(webhooks.HeaderSignature, webhooks.Sign(send.Secret, send.Payload))
	req.Header.Set(webhooks.HeaderEvent, send.EventType)
	req.Header.Set(webhooks.HeaderDelivery, send.DeliveryID)

	resp, err := webhookClient.Do(req)
	if err != nil {
		_ = wk.db.MarkWebhookFailed(ctx, send.DeliveryID, 0, err.Error())
		return err // retry
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = wk.db.MarkWebhookDelivered(ctx, send.DeliveryID, resp.StatusCode)
		return nil
	}
	_ = wk.db.MarkWebhookFailed(ctx, send.DeliveryID, resp.StatusCode,
		fmt.Sprintf("endpoint returned %d", resp.StatusCode))
	return fmt.Errorf("webhook %s returned %d", send.DeliveryID, resp.StatusCode)
}
