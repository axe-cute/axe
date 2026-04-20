package domain_test

import (
	"testing"

	"github.com/axe-cute/examples-webtoon/internal/domain"
)

// ── ValidateCreateSeries ────────────────────────────────────────────────────

func TestValidateCreateSeries_HappyPath(t *testing.T) {
	input := domain.CreateSeriesInput{
		Title:  "My Webtoon",
		Author: "Author",
		Genre:  "action",
	}
	if err := domain.ValidateCreateSeries(input); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateCreateSeries_MissingTitle(t *testing.T) {
	input := domain.CreateSeriesInput{Author: "Author"}
	if err := domain.ValidateCreateSeries(input); err == nil {
		t.Error("expected error for missing title")
	}
}

func TestValidateCreateSeries_MissingAuthor(t *testing.T) {
	input := domain.CreateSeriesInput{Title: "My Webtoon"}
	if err := domain.ValidateCreateSeries(input); err == nil {
		t.Error("expected error for missing author")
	}
}

func TestValidateCreateSeries_InvalidGenre(t *testing.T) {
	input := domain.CreateSeriesInput{
		Title:  "My Webtoon",
		Author: "Author",
		Genre:  "mystery", // not in whitelist
	}
	if err := domain.ValidateCreateSeries(input); err == nil {
		t.Error("expected error for invalid genre")
	}
}

func TestValidateCreateSeries_AllValidGenres(t *testing.T) {
	genres := []string{"action", "romance", "comedy", "drama", "fantasy", "horror", "thriller", "slice-of-life", "sci-fi", "sports", "historical"}
	for _, g := range genres {
		input := domain.CreateSeriesInput{
			Title:  "Title",
			Author: "Author",
			Genre:  g,
		}
		if err := domain.ValidateCreateSeries(input); err != nil {
			t.Errorf("expected genre %q to be valid, got: %v", g, err)
		}
	}
}

func TestValidateCreateSeries_EmptyGenreAllowed(t *testing.T) {
	input := domain.CreateSeriesInput{
		Title:  "My Webtoon",
		Author: "Author",
		Genre:  "", // empty = no genre constraint
	}
	if err := domain.ValidateCreateSeries(input); err != nil {
		t.Errorf("expected empty genre to be allowed, got: %v", err)
	}
}

func TestValidateCreateSeries_InvalidStatus(t *testing.T) {
	input := domain.CreateSeriesInput{
		Title:  "My Webtoon",
		Author: "Author",
		Status: "archived", // not in whitelist
	}
	if err := domain.ValidateCreateSeries(input); err == nil {
		t.Error("expected error for invalid status")
	}
}

// ── ValidGenres ─────────────────────────────────────────────────────────────

func TestValidGenres_Count(t *testing.T) {
	if len(domain.ValidGenres) != 11 {
		t.Errorf("expected 11 valid genres, got %d", len(domain.ValidGenres))
	}
}

// ── ValidSeriesStatuses ─────────────────────────────────────────────────────

func TestValidSeriesStatuses_AllPresent(t *testing.T) {
	expected := []string{"ongoing", "completed", "hiatus"}
	for _, s := range expected {
		if !domain.ValidSeriesStatuses[s] {
			t.Errorf("expected status %q to be valid", s)
		}
	}
}
