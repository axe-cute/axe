package domain_test

import (
	"testing"

	"github.com/axe-cute/examples-ecommerce/internal/domain"
)

func TestPagination_Default(t *testing.T) {
	p := domain.DefaultPagination()
	if p.Limit != 20 {
		t.Errorf("default limit = %d, want 20", p.Limit)
	}
	if p.Offset != 0 {
		t.Errorf("default offset = %d, want 0", p.Offset)
	}
}

func TestPagination_Validate_Valid(t *testing.T) {
	cases := []domain.Pagination{
		{Limit: 1, Offset: 0},
		{Limit: 50, Offset: 0},
		{Limit: 100, Offset: 0},
		{Limit: 20, Offset: 100},
	}
	for _, p := range cases {
		if err := p.Validate(); err != nil {
			t.Errorf("Pagination{%d,%d}.Validate() = %v, want nil", p.Limit, p.Offset, err)
		}
	}
}

func TestPagination_Validate_LimitTooLow(t *testing.T) {
	p := domain.Pagination{Limit: 0, Offset: 0}
	if err := p.Validate(); err == nil {
		t.Error("expected error for limit=0")
	}
}

func TestPagination_Validate_LimitTooHigh(t *testing.T) {
	p := domain.Pagination{Limit: 101, Offset: 0}
	if err := p.Validate(); err == nil {
		t.Error("expected error for limit=101")
	}
}

func TestPagination_Validate_NegativeOffset(t *testing.T) {
	p := domain.Pagination{Limit: 20, Offset: -1}
	if err := p.Validate(); err == nil {
		t.Error("expected error for negative offset")
	}
}
