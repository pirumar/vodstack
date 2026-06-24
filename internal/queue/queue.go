// Package queue defines the asynq task types shared by the API (producer) and
// the worker (consumer), plus a thin client wrapper.
package queue

import (
	"encoding/json"
	"errors"

	"github.com/hibiken/asynq"
)

// alreadyEnqueued reports whether an Enqueue error is the benign "this exact job
// is already pending" case rather than a real failure. asynq returns
// ErrDuplicateTask for the Unique option and ErrTaskIDConflict for the TaskID
// option (which we use for idempotency); both mean the work is already queued,
// so a re-trigger is a no-op, not a 500.
func alreadyEnqueued(err error) bool {
	return errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict)
}

const (
	// TypeTranscode is the task that turns a raw upload into an HLS ladder.
	TypeTranscode = "video:transcode"

	// TypeWebhookDeliver POSTs one recorded webhook_deliveries row to its
	// endpoint, retrying on failure.
	TypeWebhookDeliver = "webhook:deliver"

	// TypeTranscodeAV1 produces the AV1 ladder as an opt-in backfill and swaps in
	// a combined master. Always runs on the bulk lane (CPU-expensive).
	// Deprecated: superseded by TypeCodecBackfill; retained for the task-ID scheme.
	TypeTranscodeAV1 = "video:transcode_av1"

	// TypeCodecBackfill encodes every extra codec (AV1/HEVC/VP9) listed in the
	// video's encode_settings that is not yet present, then rebuilds the combined
	// master. One job per video (sequential) keeps the master rebuild race-free.
	// Always runs on the bulk lane (CPU-expensive).
	TypeCodecBackfill = "video:codec_backfill"

	// TypeAutoCaption extracts audio and runs ASR (faster-whisper) to produce a
	// caption track. Bulk lane (CPU-expensive).
	TypeAutoCaption = "video:auto_caption"

	// TypeEncrypt re-encodes a video's ladder with AES-128 and stores the key.
	// Bulk lane.
	TypeEncrypt = "video:encrypt"

	// TypeBuildSearchIndex parses a caption track into chunks, embeds them, and
	// stores the in-video search index. Bulk lane (CPU/network-bound).
	TypeBuildSearchIndex = "video:build_search_index"

	// TypeGenerateAIContent feeds the transcript to an LLM to produce a
	// description, tags, and/or chapters. Bulk lane (network-bound).
	TypeGenerateAIContent = "video:generate_ai_content"

	// TypePosterFrame grabs a single frame from the raw source at a given
	// timestamp and publishes it as the video's custom poster. Default lane (it's
	// a quick single-frame ffmpeg extract, not a full encode).
	TypePosterFrame = "video:poster_frame"

	// QueueDefault is for fresh admin uploads (higher priority); QueueBulk is
	// for migration/backfill so it never starves interactive uploads.
	// QueueWebhooks carries webhook deliveries on their own lane so retries
	// neither starve nor are starved by transcodes.
	QueueDefault  = "default"
	QueueBulk     = "bulk"
	QueueWebhooks = "webhooks"
)

// TranscodePayload is the job body for a transcode task.
//
// Exactly one source is set: SourceObject (a raw upload already in MinIO) OR
// SourceURL (fetch-from-URL ingest, e.g. migrating an existing Bunny/Vimeo
// video). For a URL the worker downloads it, stores the raw in MinIO, then
// transcodes identically.
type TranscodePayload struct {
	VideoID      string `json:"videoId"`
	LibraryID    string `json:"libraryId"`
	SourceObject string `json:"sourceObject,omitempty"`
	SourceURL    string `json:"sourceUrl,omitempty"`
}

// NewTranscodeTask builds an asynq task for the given video. opts let the
// caller pick the queue, retries, etc.
func NewTranscodeTask(p TranscodePayload, opts ...asynq.Option) (*asynq.Task, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeTranscode, body, opts...), nil
}

// Client wraps asynq.Client for enqueuing, plus an Inspector for canceling
// in-flight tasks (e.g. when a video is deleted mid-transcode).
type Client struct {
	inner     *asynq.Client
	inspector *asynq.Inspector
}

func NewClient(redisAddr string) *Client {
	opt := asynq.RedisClientOpt{Addr: redisAddr}
	return &Client{
		inner:     asynq.NewClient(opt),
		inspector: asynq.NewInspector(opt),
	}
}

func (c *Client) Close() error {
	_ = c.inspector.Close()
	return c.inner.Close()
}

// videoTaskIDs returns the deterministic asynq task IDs for all of a video's
// jobs. Keep in sync with the Enqueue* TaskID schemes below. Lang-suffixed
// caption/search tasks (e.g. "caption:<id>:en") aren't enumerated; the no-lang
// form is what the admin UI triggers.
func videoTaskIDs(videoID string) []string {
	return []string{
		"transcode:" + videoID,
		"codecs:" + videoID,
		"encrypt:" + videoID,
		"caption:" + videoID,
		"searchidx:" + videoID,
		"aicontent:" + videoID,
		"poster:" + videoID,
	}
}

// CancelVideoTasks signals cancellation for any of a video's currently-active
// jobs. asynq delivers this as a context cancellation to the running handler,
// which terminates ffmpeg (started via exec.CommandContext) promptly. Canceling
// a task that isn't active returns an error we deliberately ignore.
func (c *Client) CancelVideoTasks(videoID string) {
	for _, id := range videoTaskIDs(videoID) {
		_ = c.inspector.CancelProcessing(id)
	}
}

// enqueueIdempotent submits task, treating an in-flight duplicate as a benign
// no-op — BUT if the only thing holding the deterministic TaskID is a *dead*
// task (archived after a failed/SkipRetry run, or a retained completed one), it
// deletes that corpse and re-enqueues. Without this, a user re-trigger after a
// failure silently collapses against the archived task and never re-runs,
// stranding the operation row at "queued".
func (c *Client) enqueueIdempotent(task *asynq.Task, queueName, taskID string) error {
	_, err := c.inner.Enqueue(task)
	if err == nil {
		return nil
	}
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil // uniqueness lock held by a genuinely in-flight task
	}
	if !errors.Is(err, asynq.ErrTaskIDConflict) {
		return err
	}
	// TaskID conflict: re-run only if the existing task is dead.
	if info, ierr := c.inspector.GetTaskInfo(queueName, taskID); ierr == nil &&
		(info.State == asynq.TaskStateArchived || info.State == asynq.TaskStateCompleted) {
		_ = c.inspector.DeleteTask(queueName, taskID)
		if _, err2 := c.inner.Enqueue(task); err2 != nil && !alreadyEnqueued(err2) {
			return err2
		}
	}
	// Live duplicate (or inspection failed): collapse as idempotent.
	return nil
}

// EnqueueTranscode submits a transcode job. The asynq task ID is the video ID,
// so duplicate enqueues for the same video collapse (idempotent).
func (c *Client) EnqueueTranscode(p TranscodePayload) error {
	taskID := "transcode:" + p.VideoID
	task, err := NewTranscodeTask(p,
		asynq.MaxRetry(3),
		asynq.Queue(QueueDefault),
		asynq.TaskID(taskID),
	)
	if err != nil {
		return err
	}
	return c.enqueueIdempotent(task, QueueDefault, taskID)
}

// CodecBackfillPayload is the job body for a codec backfill (AV1/HEVC/VP9). The
// worker reads which codecs to ensure from the video's encode_settings.
type CodecBackfillPayload struct {
	VideoID   string `json:"videoId"`
	LibraryID string `json:"libraryId"`
}

// EnqueueCodecBackfill submits a codec backfill on the bulk lane so it never
// competes with interactive H.264 transcodes. Idempotent per video; a re-trigger
// after a completed/archived run (e.g. the admin adds another codec) re-runs.
func (c *Client) EnqueueCodecBackfill(p CodecBackfillPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	taskID := "codecs:" + p.VideoID
	task := asynq.NewTask(TypeCodecBackfill, body,
		asynq.MaxRetry(2),
		asynq.Queue(QueueBulk),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueBulk, taskID)
}

// EncryptPayload is the job body for an AES-128 re-encode.
type EncryptPayload struct {
	VideoID   string `json:"videoId"`
	LibraryID string `json:"libraryId"`
}

// EnqueueEncrypt submits an encryption re-encode on the bulk lane. Idempotent.
func (c *Client) EnqueueEncrypt(p EncryptPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	taskID := "encrypt:" + p.VideoID
	task := asynq.NewTask(TypeEncrypt, body,
		asynq.MaxRetry(2),
		asynq.Queue(QueueBulk),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueBulk, taskID)
}

// AutoCaptionPayload is the job body for an auto-caption run. Lang is optional
// (empty = let Whisper detect the language).
type AutoCaptionPayload struct {
	VideoID   string `json:"videoId"`
	LibraryID string `json:"libraryId"`
	Lang      string `json:"lang,omitempty"`
}

// EnqueueAutoCaption submits an auto-caption job on the bulk lane. Idempotent per
// (video, lang).
func (c *Client) EnqueueAutoCaption(p AutoCaptionPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	taskID := "caption:" + p.VideoID
	if p.Lang != "" {
		taskID += ":" + p.Lang
	}
	task := asynq.NewTask(TypeAutoCaption, body,
		asynq.MaxRetry(2),
		asynq.Queue(QueueBulk),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueBulk, taskID)
}

// BuildSearchIndexPayload is the job body for a search-index build. Lang selects
// the caption track to index (empty = let the worker pick the video's track).
type BuildSearchIndexPayload struct {
	VideoID   string `json:"videoId"`
	LibraryID string `json:"libraryId"`
	Lang      string `json:"lang,omitempty"`
}

// EnqueueBuildSearchIndex submits a search-index build on the bulk lane.
// Idempotent per (video, lang).
func (c *Client) EnqueueBuildSearchIndex(p BuildSearchIndexPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	taskID := "searchidx:" + p.VideoID
	if p.Lang != "" {
		taskID += ":" + p.Lang
	}
	task := asynq.NewTask(TypeBuildSearchIndex, body,
		asynq.MaxRetry(2),
		asynq.Queue(QueueBulk),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueBulk, taskID)
}

// GenerateAIContentPayload is the job body for LLM content generation. Kinds
// selects which artifacts to produce: "summary", "tags", "chapters".
type GenerateAIContentPayload struct {
	VideoID   string   `json:"videoId"`
	LibraryID string   `json:"libraryId"`
	Lang      string   `json:"lang,omitempty"`
	Kinds     []string `json:"kinds"`
}

// EnqueueGenerateAIContent submits an AI-content job on the bulk lane. Idempotent
// per video (a re-trigger collapses with any in-flight run).
func (c *Client) EnqueueGenerateAIContent(p GenerateAIContentPayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// Default (not bulk) lane: this job only waits on an outbound LLM HTTP call,
	// so it must not queue behind CPU-bound transcodes on the bulk lane.
	taskID := "aicontent:" + p.VideoID
	task := asynq.NewTask(TypeGenerateAIContent, body,
		asynq.MaxRetry(2),
		asynq.Queue(QueueDefault),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueDefault, taskID)
}

// PosterFramePayload is the job body for a custom-poster frame grab. AtSeconds is
// the timestamp to capture; Object is the MinIO key to write the JPEG to.
type PosterFramePayload struct {
	VideoID   string  `json:"videoId"`
	LibraryID string  `json:"libraryId"`
	AtSeconds float64 `json:"atSeconds"`
	Object    string  `json:"object"`
}

// EnqueuePosterFrame submits a poster frame-grab on the default lane. Idempotent
// per video; a re-trigger after a completed/archived run re-runs (the admin can
// pick a different frame).
func (c *Client) EnqueuePosterFrame(p PosterFramePayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	taskID := "poster:" + p.VideoID
	task := asynq.NewTask(TypePosterFrame, body,
		asynq.MaxRetry(1),
		asynq.Queue(QueueDefault),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueDefault, taskID)
}

// WebhookDeliverPayload is the job body for a webhook delivery. It carries only
// the delivery id; the worker loads the endpoint URL/secret and payload from the
// DB, so a redelivery always reflects the latest recorded state.
type WebhookDeliverPayload struct {
	DeliveryID string `json:"deliveryId"`
}

// EnqueueWebhook submits a webhook delivery job (its own queue, retried with
// asynq's exponential backoff). The task id is the delivery id so a duplicate
// enqueue for the same delivery collapses.
func (c *Client) EnqueueWebhook(deliveryID string) error {
	body, err := json.Marshal(WebhookDeliverPayload{DeliveryID: deliveryID})
	if err != nil {
		return err
	}
	taskID := "webhook:" + deliveryID
	task := asynq.NewTask(TypeWebhookDeliver, body,
		asynq.MaxRetry(10),
		asynq.Queue(QueueWebhooks),
		asynq.TaskID(taskID),
	)
	return c.enqueueIdempotent(task, QueueWebhooks, taskID)
}
