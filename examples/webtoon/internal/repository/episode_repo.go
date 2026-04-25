// Ent-backed implementation of domain.EpisodeRepository.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	ent "github.com/axe-cute/examples-webtoon/ent"
	entepisode "github.com/axe-cute/examples-webtoon/ent/episode"
	"github.com/axe-cute/examples-webtoon/internal/domain"
	"github.com/axe-cute/examples-webtoon/pkg/apperror"
)

// EpisodeRepo implements domain.EpisodeRepository using Ent.
type EpisodeRepo struct {
	client *ent.Client
	db     *sql.DB
}

// NewEpisodeRepo creates a new EpisodeRepo.
func NewEpisodeRepo(client *ent.Client, db *sql.DB) domain.EpisodeRepository {
	return &EpisodeRepo{client: client, db: db}
}

func toEpisode(e *ent.Episode) *domain.Episode {
	return &domain.Episode{
		ID:            e.ID,
		Title:         e.Title,
		EpisodeNumber: e.EpisodeNumber,
		ThumbnailUrl:  e.ThumbnailURL,
		Published:     e.Published,
		SeriesID:      e.SeriesID,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
}

func (r *EpisodeRepo) Create(ctx context.Context, input domain.CreateEpisodeInput) (*domain.Episode, error) {
	e, err := r.client.Episode.Create().
		SetTitle(input.Title).
		SetEpisodeNumber(input.EpisodeNumber).
		SetThumbnailURL(input.ThumbnailUrl).
		SetPublished(input.Published).
		SetSeriesID(input.SeriesID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("EpisodeRepo.Create: %w", err)
	}
	return toEpisode(e), nil
}

func (r *EpisodeRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Episode, error) {
	e, err := r.client.Episode.Query().Where(entepisode.IDEQ(id)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("episode not found")
		}
		return nil, fmt.Errorf("EpisodeRepo.GetByID: %w", err)
	}
	return toEpisode(e), nil
}

func (r *EpisodeRepo) Update(ctx context.Context, id uuid.UUID, input domain.UpdateEpisodeInput) (*domain.Episode, error) {
	upd := r.client.Episode.UpdateOneID(id)
	if input.Title != nil {
		upd.SetTitle(*input.Title)
	}
	if input.EpisodeNumber != nil {
		upd.SetEpisodeNumber(*input.EpisodeNumber)
	}
	if input.ThumbnailUrl != nil {
		upd.SetThumbnailURL(*input.ThumbnailUrl)
	}
	if input.Published != nil {
		upd.SetPublished(*input.Published)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("episode not found")
		}
		return nil, fmt.Errorf("EpisodeRepo.Update: %w", err)
	}
	return toEpisode(e), nil
}

func (r *EpisodeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.client.Episode.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return apperror.ErrNotFound.WithMessage("episode not found")
		}
		return fmt.Errorf("EpisodeRepo.Delete: %w", err)
	}
	return nil
}

func (r *EpisodeRepo) List(ctx context.Context, p domain.Pagination) ([]*domain.Episode, int, error) {
	total, err := r.client.Episode.Query().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("EpisodeRepo.List count: %w", err)
	}
	rows, err := r.client.Episode.Query().
		Order(ent.Desc(entepisode.FieldCreatedAt)).
		Offset(p.Offset).Limit(p.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("EpisodeRepo.List: %w", err)
	}
	out := make([]*domain.Episode, len(rows))
	for i, e := range rows {
		out[i] = toEpisode(e)
	}
	return out, total, nil
}

func (r *EpisodeRepo) ListBySeries(ctx context.Context, seriesID uuid.UUID, p domain.Pagination) ([]*domain.Episode, int, error) {
	q := r.client.Episode.Query().Where(entepisode.SeriesIDEQ(seriesID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("EpisodeRepo.ListBySeries count: %w", err)
	}
	rows, err := q.Order(ent.Asc(entepisode.FieldEpisodeNumber)).
		Offset(p.Offset).Limit(p.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("EpisodeRepo.ListBySeries: %w", err)
	}
	out := make([]*domain.Episode, len(rows))
	for i, e := range rows {
		out[i] = toEpisode(e)
	}
	return out, total, nil
}

// ── View count (raw SQL, column added outside Ent) ───────────────────────────

func (r *EpisodeRepo) IncrementViewCount(ctx context.Context, id uuid.UUID) error {
	if r.db == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx,
		"UPDATE episodes SET view_count = view_count + 1 WHERE id = $1", id)
	return err
}

// ── Likes (raw SQL) ─────────────────────────────────────────────────────────

func (r *EpisodeRepo) GetLikeCount(ctx context.Context, episodeID uuid.UUID) (int64, error) {
	if r.db == nil {
		return 0, nil
	}
	var count int64
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM episode_likes WHERE episode_id = $1", episodeID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("EpisodeRepo.GetLikeCount: %w", err)
	}
	return count, nil
}

func (r *EpisodeRepo) HasUserLiked(ctx context.Context, episodeID uuid.UUID, userID string) (bool, error) {
	if r.db == nil || userID == "" {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM episode_likes WHERE episode_id = $1 AND user_id = $2)",
		episodeID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("EpisodeRepo.HasUserLiked: %w", err)
	}
	return exists, nil
}

func (r *EpisodeRepo) ToggleLike(ctx context.Context, episodeID uuid.UUID, userID string) (bool, int64, error) {
	if r.db == nil {
		return false, 0, fmt.Errorf("database not available")
	}
	liked, err := r.HasUserLiked(ctx, episodeID, userID)
	if err != nil {
		return false, 0, err
	}
	if liked {
		_, err = r.db.ExecContext(ctx,
			"DELETE FROM episode_likes WHERE episode_id = $1 AND user_id = $2",
			episodeID, userID)
		if err != nil {
			return false, 0, fmt.Errorf("EpisodeRepo.ToggleLike delete: %w", err)
		}
	} else {
		_, err = r.db.ExecContext(ctx,
			"INSERT INTO episode_likes (episode_id, user_id) VALUES ($1, $2)",
			episodeID, userID)
		if err != nil {
			return false, 0, fmt.Errorf("EpisodeRepo.ToggleLike insert: %w", err)
		}
	}
	count, err := r.GetLikeCount(ctx, episodeID)
	return !liked, count, err
}

// ── Comments (raw SQL) ────────────────────────────────────────────────────

// ListComments returns paginated top-level comments together with all
// replies attached to those threads. Replies are grouped by their
// root_comment_id (the top-level ancestor), but parent_comment_id is
// preserved so the UI can render @user mentions for replies-to-replies.
// `total` counts only top-level comments to keep pagination stable.
func (r *EpisodeRepo) ListComments(ctx context.Context, episodeID uuid.UUID, p domain.Pagination) ([]*domain.EpisodeComment, int, error) {
	if r.db == nil {
		return nil, 0, nil
	}
	var total int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM episode_comments WHERE episode_id = $1 AND parent_comment_id IS NULL",
		episodeID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("EpisodeRepo.ListComments count: %w", err)
	}
	// Page top-level comments newest-first, then attach every reply whose
	// root_comment_id matches one of those tops. The LEFT JOIN to the
	// parent comment row exposes parent_user_id for @mentions.
	const q = `
		WITH top AS (
			SELECT id, created_at
			FROM episode_comments
			WHERE episode_id = $1 AND parent_comment_id IS NULL
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		)
		SELECT c.id, c.episode_id, c.user_id, c.content,
		       c.parent_comment_id, c.root_comment_id,
		       COALESCE(p.user_id, ''),
		       c.created_at, c.updated_at,
		       COALESCE(c.root_comment_id, c.id) AS thread_root,
		       COALESCE(t_root.created_at, c.created_at) AS thread_created_at,
		       CASE WHEN c.parent_comment_id IS NULL THEN 0 ELSE 1 END AS depth
		FROM episode_comments c
		LEFT JOIN episode_comments p ON p.id = c.parent_comment_id
		LEFT JOIN top t_root ON t_root.id = COALESCE(c.root_comment_id, c.id)
		WHERE c.episode_id = $1
		  AND (
		    (c.parent_comment_id IS NULL AND c.id IN (SELECT id FROM top))
		    OR c.root_comment_id IN (SELECT id FROM top)
		  )
		ORDER BY thread_created_at DESC, thread_root, depth ASC, c.created_at ASC`
	rows, err := r.db.QueryContext(ctx, q, episodeID, p.Limit, p.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("EpisodeRepo.ListComments: %w", err)
	}
	defer rows.Close()
	out := make([]*domain.EpisodeComment, 0, p.Limit)
	for rows.Next() {
		var c domain.EpisodeComment
		var parent, root sql.NullString
		var threadRoot uuid.UUID
		var threadCreatedAt time.Time
		var depth int
		if err := rows.Scan(
			&c.ID, &c.EpisodeID, &c.UserID, &c.Content,
			&parent, &root, &c.ParentUserID,
			&c.CreatedAt, &c.UpdatedAt,
			&threadRoot, &threadCreatedAt, &depth,
		); err != nil {
			return nil, 0, fmt.Errorf("EpisodeRepo.ListComments scan: %w", err)
		}
		if parent.Valid {
			if pid, err := uuid.Parse(parent.String); err == nil {
				c.ParentCommentID = &pid
			}
		}
		if root.Valid {
			if rid, err := uuid.Parse(root.String); err == nil {
				c.RootCommentID = &rid
			}
		}
		out = append(out, &c)
	}
	return out, total, nil
}

// ── Comment likes (raw SQL) ───────────────────────────────────────────────

func (r *EpisodeRepo) ToggleCommentLike(ctx context.Context, commentID uuid.UUID, userID string) (bool, int64, error) {
	if r.db == nil {
		return false, 0, fmt.Errorf("database not available")
	}
	if userID == "" {
		return false, 0, apperror.ErrUnauthorized
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM episode_comment_likes WHERE comment_id = $1 AND user_id = $2)",
		commentID, userID).Scan(&exists); err != nil {
		return false, 0, fmt.Errorf("EpisodeRepo.ToggleCommentLike check: %w", err)
	}
	if exists {
		if _, err := r.db.ExecContext(ctx,
			"DELETE FROM episode_comment_likes WHERE comment_id = $1 AND user_id = $2",
			commentID, userID); err != nil {
			return false, 0, fmt.Errorf("EpisodeRepo.ToggleCommentLike delete: %w", err)
		}
	} else {
		if _, err := r.db.ExecContext(ctx,
			"INSERT INTO episode_comment_likes (comment_id, user_id) VALUES ($1, $2)",
			commentID, userID); err != nil {
			return false, 0, fmt.Errorf("EpisodeRepo.ToggleCommentLike insert: %w", err)
		}
	}
	var count int64
	if err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM episode_comment_likes WHERE comment_id = $1", commentID).Scan(&count); err != nil {
		return false, 0, fmt.Errorf("EpisodeRepo.ToggleCommentLike count: %w", err)
	}
	return !exists, count, nil
}

func (r *EpisodeRepo) GetCommentLikeInfo(ctx context.Context, commentIDs []uuid.UUID, userID string) (map[uuid.UUID]domain.CommentLikeInfo, error) {
	out := make(map[uuid.UUID]domain.CommentLikeInfo, len(commentIDs))
	if r.db == nil || len(commentIDs) == 0 {
		return out, nil
	}
	// Build $1,$2,... placeholders
	args := make([]any, 0, len(commentIDs))
	placeholders := make([]byte, 0, len(commentIDs)*4)
	for i, id := range commentIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '$')
		placeholders = append(placeholders, []byte(fmt.Sprintf("%d", i+1))...)
		args = append(args, id)
		out[id] = domain.CommentLikeInfo{CommentID: id}
	}
	// Counts
	countQ := fmt.Sprintf(
		"SELECT comment_id, COUNT(*) FROM episode_comment_likes WHERE comment_id IN (%s) GROUP BY comment_id",
		string(placeholders))
	rows, err := r.db.QueryContext(ctx, countQ, args...)
	if err != nil {
		return nil, fmt.Errorf("EpisodeRepo.GetCommentLikeInfo counts: %w", err)
	}
	for rows.Next() {
		var cid uuid.UUID
		var n int64
		if err := rows.Scan(&cid, &n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("EpisodeRepo.GetCommentLikeInfo scan: %w", err)
		}
		info := out[cid]
		info.LikeCount = n
		out[cid] = info
	}
	rows.Close()
	if userID == "" {
		return out, nil
	}
	// Which of these comments has the user liked? userID is $1, comment ids start at $2.
	likedPH := make([]byte, 0, len(commentIDs)*4)
	likedArgs := make([]any, 0, len(commentIDs)+1)
	likedArgs = append(likedArgs, userID)
	for i, id := range commentIDs {
		if i > 0 {
			likedPH = append(likedPH, ',')
		}
		likedPH = append(likedPH, '$')
		likedPH = append(likedPH, []byte(fmt.Sprintf("%d", i+2))...)
		likedArgs = append(likedArgs, id)
	}
	likedQ := fmt.Sprintf(
		"SELECT comment_id FROM episode_comment_likes WHERE user_id = $1 AND comment_id IN (%s)",
		string(likedPH))
	rows2, err := r.db.QueryContext(ctx, likedQ, likedArgs...)
	if err != nil {
		return nil, fmt.Errorf("EpisodeRepo.GetCommentLikeInfo liked: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var cid uuid.UUID
		if err := rows2.Scan(&cid); err != nil {
			return nil, fmt.Errorf("EpisodeRepo.GetCommentLikeInfo liked scan: %w", err)
		}
		info := out[cid]
		info.UserLiked = true
		out[cid] = info
	}
	return out, nil
}

func (r *EpisodeRepo) CreateComment(ctx context.Context, episodeID uuid.UUID, userID string, content string, parentID *uuid.UUID) (*domain.EpisodeComment, error) {
	if r.db == nil {
		return nil, fmt.Errorf("database not available")
	}
	if content == "" {
		return nil, apperror.ErrInvalidInput.WithMessage("comment content is required")
	}
	// For replies, the *direct* parent is preserved (so the UI can render
	// "@author_of_parent" for nested replies), and root_comment_id points at
	// the top-level ancestor. The visual tree stays 1 level deep — replies
	// are simply rendered chronologically under the root.
	var (
		parentCommentID *uuid.UUID
		rootCommentID   *uuid.UUID
		parentUserID    string
	)
	if parentID != nil && *parentID != uuid.Nil {
		var parentEpisode uuid.UUID
		var parentRoot sql.NullString
		var pUserID string
		err := r.db.QueryRowContext(ctx,
			"SELECT episode_id, root_comment_id, user_id FROM episode_comments WHERE id = $1",
			*parentID).Scan(&parentEpisode, &parentRoot, &pUserID)
		if err != nil {
			return nil, apperror.ErrInvalidInput.WithMessage("parent comment not found")
		}
		if parentEpisode != episodeID {
			return nil, apperror.ErrInvalidInput.WithMessage("parent comment does not belong to this episode")
		}
		parentCommentID = parentID
		parentUserID = pUserID
		if parentRoot.Valid {
			if rid, err := uuid.Parse(parentRoot.String); err == nil {
				rootCommentID = &rid
			}
		}
		// Parent is itself a top-level comment → it becomes the root.
		if rootCommentID == nil {
			rootCommentID = parentID
		}
	}
	var id uuid.UUID
	var createdAt, updatedAt time.Time
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO episode_comments (episode_id, user_id, content, parent_comment_id, root_comment_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`,
		episodeID, userID, content, parentCommentID, rootCommentID,
	).Scan(&id, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("EpisodeRepo.CreateComment: %w", err)
	}
	return &domain.EpisodeComment{
		ID:              id,
		EpisodeID:       episodeID,
		UserID:          userID,
		Content:         content,
		ParentCommentID: parentCommentID,
		RootCommentID:   rootCommentID,
		ParentUserID:    parentUserID,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}
