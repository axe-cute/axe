// Ent-backed implementation of domain.BookmarkRepository.
package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	ent "github.com/axe-cute/examples-webtoon/ent"
	entbookmark "github.com/axe-cute/examples-webtoon/ent/bookmark"
	"github.com/axe-cute/examples-webtoon/internal/domain"
	"github.com/axe-cute/examples-webtoon/pkg/apperror"
)

// BookmarkRepo implements domain.BookmarkRepository using Ent.
type BookmarkRepo struct {
	client *ent.Client
}

// NewBookmarkRepo creates a new BookmarkRepo.
func NewBookmarkRepo(client *ent.Client) domain.BookmarkRepository {
	return &BookmarkRepo{client: client}
}

func toBookmark(b *ent.Bookmark) *domain.Bookmark {
	return &domain.Bookmark{
		ID:        b.ID,
		UserID:    b.UserID,
		SeriesID:  b.SeriesID,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

func (r *BookmarkRepo) Create(ctx context.Context, input domain.CreateBookmarkInput) (*domain.Bookmark, error) {
	b, err := r.client.Bookmark.Create().
		SetUserID(input.UserID).
		SetSeriesID(input.SeriesID).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("BookmarkRepo.Create: %w", err)
	}
	return toBookmark(b), nil
}

func (r *BookmarkRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Bookmark, error) {
	b, err := r.client.Bookmark.Query().Where(entbookmark.IDEQ(id)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("bookmark not found")
		}
		return nil, fmt.Errorf("BookmarkRepo.GetByID: %w", err)
	}
	return toBookmark(b), nil
}

func (r *BookmarkRepo) Update(ctx context.Context, id uuid.UUID, input domain.UpdateBookmarkInput) (*domain.Bookmark, error) {
	upd := r.client.Bookmark.UpdateOneID(id)
	if input.SeriesID != nil {
		upd.SetSeriesID(*input.SeriesID)
	}
	b, err := upd.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("bookmark not found")
		}
		return nil, fmt.Errorf("BookmarkRepo.Update: %w", err)
	}
	return toBookmark(b), nil
}

func (r *BookmarkRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.client.Bookmark.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return apperror.ErrNotFound.WithMessage("bookmark not found")
		}
		return fmt.Errorf("BookmarkRepo.Delete: %w", err)
	}
	return nil
}

func (r *BookmarkRepo) List(ctx context.Context, p domain.Pagination) ([]*domain.Bookmark, int, error) {
	total, err := r.client.Bookmark.Query().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("BookmarkRepo.List count: %w", err)
	}
	rows, err := r.client.Bookmark.Query().
		Order(ent.Desc(entbookmark.FieldCreatedAt)).
		Offset(p.Offset).Limit(p.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("BookmarkRepo.List: %w", err)
	}
	out := make([]*domain.Bookmark, len(rows))
	for i, b := range rows {
		out[i] = toBookmark(b)
	}
	return out, total, nil
}

func (r *BookmarkRepo) ListByUser(ctx context.Context, userID string, p domain.Pagination) ([]*domain.Bookmark, int, error) {
	q := r.client.Bookmark.Query().Where(entbookmark.UserIDEQ(userID))
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("BookmarkRepo.ListByUser count: %w", err)
	}
	rows, err := q.Order(ent.Desc(entbookmark.FieldCreatedAt)).
		Offset(p.Offset).Limit(p.Limit).All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("BookmarkRepo.ListByUser: %w", err)
	}
	out := make([]*domain.Bookmark, len(rows))
	for i, b := range rows {
		out[i] = toBookmark(b)
	}
	return out, total, nil
}

func (r *BookmarkRepo) FindByUserAndSeries(ctx context.Context, userID string, seriesID uuid.UUID) (*domain.Bookmark, error) {
	b, err := r.client.Bookmark.Query().
		Where(entbookmark.UserIDEQ(userID), entbookmark.SeriesIDEQ(seriesID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, apperror.ErrNotFound.WithMessage("bookmark not found")
		}
		return nil, fmt.Errorf("BookmarkRepo.FindByUserAndSeries: %w", err)
	}
	return toBookmark(b), nil
}
