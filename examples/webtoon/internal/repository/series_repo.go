// Ent-backed implementation of domain.SeriesRepository.
//
// A subset of queries (ListTrending, ListFiltered) use raw SQL via *sql.DB
// because trending_score is a column managed outside the Ent schema — see
// db/migrations/20260424180000_scale_indexes_and_trending.sql.
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	ent "github.com/axe-cute/examples-webtoon/ent"
	entseries "github.com/axe-cute/examples-webtoon/ent/series"
	"github.com/axe-cute/examples-webtoon/internal/domain"
	"github.com/axe-cute/examples-webtoon/pkg/apperror"
)

// SeriesRepo implements domain.SeriesRepository using Ent for standard CRUD
// and raw *sql.DB for trending queries.
type SeriesRepo struct {
	client *ent.Client
	db     *sql.DB
}

// NewSeriesRepo creates a new SeriesRepo. Pass the same *sql.DB that backs the
// Ent driver so all queries share the pool.
func NewSeriesRepo(client *ent.Client, db *sql.DB) domain.SeriesRepository {
	return &SeriesRepo{client: client, db: db}
}

func toSeries(e *ent.Series) *domain.Series {
	return &domain.Series{
		ID:          e.ID,
		Title:       e.Title,
		Description: e.Description,
		Genre:       e.Genre,
		Author:      e.Author,
		CoverUrl:    e.CoverURL,
		Status:      e.Status,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

func (r *SeriesRepo) Create(ctx context.Context, input domain.CreateSeriesInput) (*domain.Series, error) {
	e, err := r.client.Series.Create().
		SetTitle(input.Title).
		SetDescription(input.Description).
		SetGenre(input.Genre).
		SetAuthor(input.Author).
		SetCoverURL(input.CoverUrl).
		SetStatus(input.Status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("SeriesRepo.Create: %w", err)
	}
	return toSeries(e), nil
}

func (r *SeriesRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Series, error) {
	e, err := r.client.Series.Query().Where(entseries.IDEQ(id)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("series not found")
		}
		return nil, fmt.Errorf("SeriesRepo.GetByID: %w", err)
	}
	return toSeries(e), nil
}

func (r *SeriesRepo) Update(ctx context.Context, id uuid.UUID, input domain.UpdateSeriesInput) (*domain.Series, error) {
	upd := r.client.Series.UpdateOneID(id)
	if input.Title != nil {
		upd.SetTitle(*input.Title)
	}
	if input.Description != nil {
		upd.SetDescription(*input.Description)
	}
	if input.Genre != nil {
		upd.SetGenre(*input.Genre)
	}
	if input.Author != nil {
		upd.SetAuthor(*input.Author)
	}
	if input.CoverUrl != nil {
		upd.SetCoverURL(*input.CoverUrl)
	}
	if input.Status != nil {
		upd.SetStatus(*input.Status)
	}
	e, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("series not found")
		}
		return nil, fmt.Errorf("SeriesRepo.Update: %w", err)
	}
	return toSeries(e), nil
}

func (r *SeriesRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.client.Series.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return apperror.ErrNotFound.WithMessage("series not found")
		}
		return fmt.Errorf("SeriesRepo.Delete: %w", err)
	}
	return nil
}

func (r *SeriesRepo) List(ctx context.Context, p domain.Pagination) ([]*domain.Series, int, error) {
	return r.ListFiltered(ctx, domain.SeriesFilter{}, p)
}

// ListFiltered narrows by genre/status. Uses the composite index
// idx_series_genre_status_created for the common browse query.
func (r *SeriesRepo) ListFiltered(ctx context.Context, f domain.SeriesFilter, p domain.Pagination) ([]*domain.Series, int, error) {
	q := r.client.Series.Query()
	if f.Genre != "" {
		q = q.Where(entseries.GenreEQ(f.Genre))
	}
	if f.Status != "" {
		q = q.Where(entseries.StatusEQ(f.Status))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("SeriesRepo.ListFiltered count: %w", err)
	}
	rows, err := q.Order(ent.Desc(entseries.FieldCreatedAt)).
		Offset(p.Offset).
		Limit(p.Limit).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("SeriesRepo.ListFiltered: %w", err)
	}
	out := make([]*domain.Series, len(rows))
	for i, e := range rows {
		out[i] = toSeries(e)
	}
	return out, total, nil
}

// ListTrending returns the top N series ordered by trending_score DESC.
// Uses the partial index idx_series_trending for sub-ms lookups.
func (r *SeriesRepo) ListTrending(ctx context.Context, limit int) ([]*domain.Series, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	const q = `
		SELECT id, title, description, genre, author, cover_url, status,
		       trending_score, created_at, updated_at
		  FROM series
		 WHERE trending_score > 0
		 ORDER BY trending_score DESC
		 LIMIT $1`

	rows, err := r.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("SeriesRepo.ListTrending: %w", err)
	}
	defer rows.Close()

	var out []*domain.Series
	for rows.Next() {
		var s domain.Series
		if err := rows.Scan(
			&s.ID, &s.Title, &s.Description, &s.Genre, &s.Author,
			&s.CoverUrl, &s.Status, &s.TrendingScore,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("SeriesRepo.ListTrending scan: %w", err)
		}
		out = append(out, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SeriesRepo.ListTrending iter: %w", err)
	}
	return out, nil
}
