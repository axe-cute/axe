package domain

import "errors"

// Pagination holds offset-based pagination parameters.
type Pagination struct {
	Limit  int
	Offset int
}

// DefaultPagination returns sensible defaults (20 items, offset 0).
func DefaultPagination() Pagination { return Pagination{Limit: 20, Offset: 0} }

// Validate checks that pagination values are within allowed bounds.
func (p Pagination) Validate() error {
	if p.Limit < 1 || p.Limit > 100 {
		return errors.New("limit must be between 1 and 100")
	}
	if p.Offset < 0 {
		return errors.New("offset must be non-negative")
	}
	return nil
}
