package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"golang.org/x/image/webp"

	"github.com/axe-cute/examples-webtoon/pkg/storage"
)

// TaskTransformPage is enqueued by the admin upload endpoint once an original
// image is confirmed in storage. The worker produces resized JPEG variants
// (thumbnail + medium) and flips the DB row to status='ready'.
const TaskTransformPage = "webtoon:page:transform"

// JPEG quality for output variants. 85 is the industry standard sweet
// spot — visually indistinguishable from the original, ~90% smaller than
// typical camera JPEGs.
const jpegQuality = 85

// Variant widths (pixels). Heights preserve aspect ratio.
const (
	thumbWidth  = 400
	mediumWidth = 1200
)

// TransformPayload is the asynq task payload. Kept minimal — the worker
// reloads the page row from the DB to get the latest state.
type TransformPayload struct {
	PageID uuid.UUID `json:"page_id"`
}

// NewTransformPageTask builds an asynq task for a given page.
func NewTransformPageTask(pageID uuid.UUID) (*asynq.Task, error) {
	payload, err := json.Marshal(TransformPayload{PageID: pageID})
	if err != nil {
		return nil, err
	}
	// default queue, MaxRetry 3 — transient storage/image errors deserve
	// a few retries before we flip the row to failed.
	return asynq.NewTask(TaskTransformPage, payload,
		asynq.MaxRetry(3),
		asynq.Timeout(60*time.Second),
	), nil
}

// TransformPageHandler decodes the payload, reads the original from
// storage, produces variants, writes them back, and updates the DB row.
//
// Design notes:
//   - Idempotent: safe to re-run. If variant keys already exist, overwrite.
//   - Never mutates the original. If the transform fails, original stays.
//   - Status transitions are atomic via a single UPDATE.
func TransformPageHandler(db *sql.DB, s *storage.Client, log *slog.Logger) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p TransformPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			// Bad payload — don't retry, no point.
			return fmt.Errorf("%w: %v", asynq.SkipRetry, err)
		}
		return transformPage(ctx, db, s, log, p.PageID)
	}
}

func transformPage(ctx context.Context, db *sql.DB, s *storage.Client, log *slog.Logger, pageID uuid.UUID) error {
	// Load the row. The episode_id is needed to compute output keys.
	var (
		episodeID   uuid.UUID
		pageNum     int
		originalKey string
		contentType sql.NullString
		status      string
	)
	if err := db.QueryRowContext(ctx, `
		SELECT episode_id, page_num, original_key, content_type, status
		  FROM episode_pages WHERE id = $1`, pageID,
	).Scan(&episodeID, &pageNum, &originalKey, &contentType, &status); err != nil {
		if err == sql.ErrNoRows {
			log.Warn("transform: page not found, skipping", "page_id", pageID)
			return nil // row was deleted mid-flight; drop the task
		}
		return fmt.Errorf("load page: %w", err)
	}

	// Flip to processing so the admin UI reflects progress. Ignore error
	// if another worker beat us — MaxRetry + unique queue dedup will handle it.
	_, _ = db.ExecContext(ctx,
		`UPDATE episode_pages SET status='processing', updated_at=NOW() WHERE id=$1 AND status IN ('uploaded','failed')`,
		pageID)

	// Fetch + decode.
	rc, err := s.Get(ctx, originalKey)
	if err != nil {
		return markFailed(ctx, db, pageID, fmt.Errorf("fetch original: %w", err))
	}
	defer rc.Close()

	img, err := decodeImage(rc, contentType.String)
	if err != nil {
		return markFailed(ctx, db, pageID, fmt.Errorf("decode: %w", err))
	}
	bounds := img.Bounds()
	origW, origH := bounds.Dx(), bounds.Dy()

	// Resize + encode variants. Lanczos is slightly slower than Linear but
	// produces noticeably sharper text (critical for manga/webtoon).
	thumbKey := variantKey(episodeID, pageNum, "thumb")
	mediumKey := variantKey(episodeID, pageNum, "medium")

	if err := resizeAndUpload(ctx, s, img, thumbWidth, thumbKey); err != nil {
		return markFailed(ctx, db, pageID, fmt.Errorf("thumb: %w", err))
	}
	if err := resizeAndUpload(ctx, s, img, mediumWidth, mediumKey); err != nil {
		return markFailed(ctx, db, pageID, fmt.Errorf("medium: %w", err))
	}

	// Single atomic update. Clients polling the page list will flip from
	// "pending" to "ready" on the next fetch.
	if _, err := db.ExecContext(ctx, `
		UPDATE episode_pages
		   SET thumb_key=$2, medium_key=$3,
		       width_px=$4, height_px=$5,
		       status='ready', error=NULL, updated_at=NOW()
		 WHERE id=$1`,
		pageID, thumbKey, mediumKey, origW, origH,
	); err != nil {
		return markFailed(ctx, db, pageID, fmt.Errorf("finalize: %w", err))
	}

	log.Info("page transformed",
		"page_id", pageID,
		"episode_id", episodeID,
		"page_num", pageNum,
		"width", origW,
		"height", origH,
	)
	return nil
}

// decodeImage handles the formats the admin allows: JPEG, PNG, WebP.
// We trust the server-validated content_type over sniffing — it was
// already bound into the presigned-URL signature at upload time.
func decodeImage(r io.Reader, contentType string) (image.Image, error) {
	switch strings.ToLower(contentType) {
	case "image/webp":
		return webp.Decode(r)
	default:
		// image.Decode covers JPEG and PNG (PNG registered via side-effect
		// import above). Unknown types fall through with a clear error.
		img, _, err := image.Decode(r)
		return img, err
	}
}

// resizeAndUpload scales DOWN to the target width (preserves aspect ratio)
// and writes a JPEG to storage under key. If the source is already narrower
// than the target, we skip resizing — upscaling wastes CPU and quality.
func resizeAndUpload(ctx context.Context, s *storage.Client, img image.Image, targetWidth int, key string) error {
	out := img
	if img.Bounds().Dx() > targetWidth {
		out = imaging.Resize(img, targetWidth, 0, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	return s.Put(ctx, key, "image/jpeg", &buf, int64(buf.Len()))
}

// variantKey produces the deterministic, immutable key for a variant.
// Storing under a stable path means we can set very long cache headers
// and trust CDN caching.
func variantKey(episodeID uuid.UUID, pageNum int, variant string) string {
	return path.Join("pages", episodeID.String(), fmt.Sprintf("%04d-%s.jpg", pageNum, variant))
}

// markFailed flips the row to failed and writes the error for the admin
// dashboard. Returns the original error (wrapped) so asynq retries.
func markFailed(ctx context.Context, db *sql.DB, pageID uuid.UUID, cause error) error {
	_, _ = db.ExecContext(ctx,
		`UPDATE episode_pages SET status='failed', error=$2, updated_at=NOW() WHERE id=$1`,
		pageID, cause.Error())
	return cause
}
