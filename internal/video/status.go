// Package video holds the video domain model and its status state machine.
//
// Status integers deliberately mirror Bunny Stream's so a frontend that already
// understands those codes needs no remapping when this service is wired in:
//
//	0 Created     - record exists, awaiting upload
//	1 Uploaded    - raw source in object storage, transcode enqueued
//	2 Processing  - worker probing / preparing
//	3 Transcoding - ffmpeg running (encode_progress is meaningful)
//	4 Finished    - HLS published, ready to play
//	5 Failed      - terminal error (error_message set)
package video

import (
	"encoding/json"
	"time"
)

type Status int

const (
	StatusCreated     Status = 0
	StatusUploaded    Status = 1
	StatusProcessing  Status = 2
	StatusTranscoding Status = 3
	StatusFinished    Status = 4
	StatusFailed      Status = 5
)

// IsReady reports whether the video can be played. Matches Bunny Stream
// semantics (ready when status == 3 || 4).
func (s Status) IsReady() bool {
	return s == StatusTranscoding || s == StatusFinished
}

func (s Status) String() string {
	switch s {
	case StatusCreated:
		return "created"
	case StatusUploaded:
		return "uploaded"
	case StatusProcessing:
		return "processing"
	case StatusTranscoding:
		return "transcoding"
	case StatusFinished:
		return "finished"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Video is the metadata record owned by this service. A downstream application
// typically stores only a pointer (StreamVideoId/StreamLibraryId) back to this row.
type Video struct {
	ID                   string  `json:"videoId"`
	LibraryID            string  `json:"libraryId"`
	Title                string  `json:"title"`
	CollectionID         *string `json:"collectionId,omitempty"`
	FolderID             *string `json:"folderId,omitempty"`
	Status               Status  `json:"status"`
	SourceObject         *string `json:"-"`
	DurationSeconds      *int    `json:"length,omitempty"`
	Width                *int    `json:"width,omitempty"`
	Height               *int    `json:"height,omitempty"`
	SizeBytes            *int64  `json:"storageSize,omitempty"`
	AvailableResolutions *string `json:"availableResolutions,omitempty"`
	ThumbnailFile        *string `json:"thumbnailFileName,omitempty"`
	EncodeProgress       int     `json:"encodeProgress"`
	ErrorMessage         *string `json:"errorMessage,omitempty"`
	ThumbnailsVTT        *string `json:"-"`
	// Description is an LLM-generated summary/description; nil when unset.
	Description *string `json:"description,omitempty"`
	// Tags are LLM-generated content keywords (empty slice when unset).
	Tags []string `json:"tags"`
	// Chapters is a raw JSON array of {start:int, title:string}; nil when unset.
	Chapters json.RawMessage `json:"chapters,omitempty"`
	// DeletedAt is set only for trashed videos (populated by trash listings).
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// Chapter is one YouTube-style section marker.
type Chapter struct {
	Start int    `json:"start"` // seconds
	Title string `json:"title"`
}

// Caption is a subtitle track for a video.
type Caption struct {
	Lang   string `json:"lang"`
	Label  string `json:"label"`
	Object string `json:"-"` // MinIO object key
}
