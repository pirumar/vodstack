// Package storage wraps minio-go for the object operations this service needs:
// presigned uploads, recursive directory upload of HLS output, downloading the
// raw source for transcoding, and prefix deletion.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client *minio.Client
	bucket string
}

func New(endpoint, accessKey, secretKey string, useSSL bool, bucket string) (*Store, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}
	return &Store{client: client, bucket: bucket}, nil
}

// EnsureBucket creates the bucket if it does not exist. Tolerant of the bucket
// already existing (race on startup).
func (s *Store) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		// Another instance may have created it between the check and now.
		if exists2, e2 := s.client.BucketExists(ctx, s.bucket); e2 == nil && exists2 {
			return nil
		}
		return err
	}
	return nil
}

// RawObjectKey is the canonical key for a video's uploaded source file.
func RawObjectKey(videoID string) string {
	return fmt.Sprintf("raw/%s/source", videoID)
}

// HLSPrefix is the published output prefix for a video.
func HLSPrefix(videoID string) string {
	return fmt.Sprintf("hls/%s/", videoID)
}

// PresignedPut returns a URL the client can PUT the raw source to directly.
func (s *Store) PresignedPut(ctx context.Context, object string, ttl time.Duration) (*url.URL, error) {
	return s.client.PresignedPutObject(ctx, s.bucket, object, ttl)
}

// PutStream streams a reader directly to an object (used by the admin panel's
// proxied upload). size may be -1 when unknown; minio-go then buffers in parts.
func (s *Store) PutStream(ctx context.Context, object string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, object, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// StatObject returns size/metadata; used to confirm an upload completed.
func (s *Store) StatObject(ctx context.Context, object string) (minio.ObjectInfo, error) {
	return s.client.StatObject(ctx, s.bucket, object, minio.StatObjectOptions{})
}

// DownloadToFile streams an object to a local path (for transcoding).
func (s *Store) DownloadToFile(ctx context.Context, object, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return s.client.FGetObject(ctx, s.bucket, object, localPath, minio.GetObjectOptions{})
}

// UploadDir recursively uploads every file under localDir to keyPrefix,
// preserving relative paths. Content-Type is set so HLS playlists/segments
// are served correctly by nginx/MinIO.
func (s *Store) UploadDir(ctx context.Context, localDir, keyPrefix string) error {
	return filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		key := keyPrefix + filepath.ToSlash(rel)
		_, err = s.client.FPutObject(ctx, s.bucket, key, path, minio.PutObjectOptions{
			ContentType: contentTypeFor(path),
		})
		return err
	})
}

// UploadFile uploads a single local file to an object key.
func (s *Store) UploadFile(ctx context.Context, object, localPath, contentType string) error {
	_, err := s.client.FPutObject(ctx, s.bucket, object, localPath, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// GetBytes reads a whole object into memory (used for small files like the
// master playlist when rewriting it for the AV1 backfill).
func (s *Store) GetBytes(ctx context.Context, object string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, object, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	return io.ReadAll(obj)
}

// RemoveObject deletes a single object (best-effort; no error if missing).
func (s *Store) RemoveObject(ctx context.Context, object string) error {
	return s.client.RemoveObject(ctx, s.bucket, object, minio.RemoveObjectOptions{})
}

// PutBytes writes an in-memory blob to an object (used for generated VTTs).
func (s *Store) PutBytes(ctx context.Context, object string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, object, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

// RemovePrefix deletes every object under the given prefix.
func (s *Store) RemovePrefix(ctx context.Context, prefix string) error {
	objectsCh := make(chan minio.ObjectInfo)
	go func() {
		defer close(objectsCh)
		for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		}) {
			if obj.Err != nil {
				continue
			}
			objectsCh <- obj
		}
	}()
	for rErr := range s.client.RemoveObjects(ctx, s.bucket, objectsCh, minio.RemoveObjectsOptions{}) {
		if rErr.Err != nil {
			return rErr.Err
		}
	}
	return nil
}

func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".m4s":
		return "video/iso.segment"
	case ".ts":
		return "video/mp2t"
	case ".mp4":
		return "video/mp4"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".vtt":
		return "text/vtt"
	default:
		return "application/octet-stream"
	}
}
