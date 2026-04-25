// Package migrate — unit tests for the SQL marker parser.
//
// Regression target: a migration file containing both `-- +migrate Up` and
// `-- +migrate Down` blocks must NEVER execute the Down section when run
// via `axe migrate up`. We lost a column to this exact bug while building
// examples/webtoon (see _internal/roadmap-evidence.md, row E1).
package migrate

import (
	"strings"
	"testing"
)

func TestSplitUpDown_NoMarkers_ReturnsWholeFileAsUp(t *testing.T) {
	const sql = `ALTER TABLE foo ADD COLUMN bar INT;
CREATE INDEX idx_foo_bar ON foo (bar);`

	up, down := splitUpDown(sql)

	if up != sql {
		t.Errorf("up: want full SQL, got %q", up)
	}
	if down != "" {
		t.Errorf("down: want empty for no-marker file, got %q", down)
	}
}

func TestSplitUpDown_UpOnly(t *testing.T) {
	const sql = `-- Migration: add_bar
-- +migrate Up
ALTER TABLE foo ADD COLUMN bar INT;
CREATE INDEX idx_foo_bar ON foo (bar);
`

	up, down := splitUpDown(sql)

	if !strings.Contains(up, "ADD COLUMN bar") {
		t.Errorf("up missing ADD COLUMN: %q", up)
	}
	if strings.Contains(up, "+migrate") {
		t.Errorf("up must not contain marker line: %q", up)
	}
	if down != "" {
		t.Errorf("down: want empty when no Down section, got %q", down)
	}
}

// THE regression. If this fails, `axe migrate up` will execute Down SQL
// and we will once again drop production columns. Do not skip.
func TestSplitUpDown_BothSections_DownIsIsolated(t *testing.T) {
	const sql = `-- Migration: add_bar
-- +migrate Up
ALTER TABLE foo ADD COLUMN bar INT;
-- +migrate Down
ALTER TABLE foo DROP COLUMN bar;
`

	up, down := splitUpDown(sql)

	if !strings.Contains(up, "ADD COLUMN bar") {
		t.Errorf("up missing ADD COLUMN: %q", up)
	}
	if strings.Contains(up, "DROP COLUMN") {
		t.Fatalf("REGRESSION: up section leaked the Down SQL.\nup = %q", up)
	}
	if !strings.Contains(down, "DROP COLUMN bar") {
		t.Errorf("down missing DROP COLUMN: %q", down)
	}
}

func TestSplitUpDown_DownOnly_UpIsEmpty(t *testing.T) {
	// A file with only Down is malformed for `up`, but the parser still
	// has to behave: empty up, populated down. The caller (applyMigration)
	// turns empty up into a clear error message.
	const sql = `-- +migrate Down
ALTER TABLE foo DROP COLUMN bar;
`

	up, down := splitUpDown(sql)

	if up != "" {
		t.Errorf("up: want empty when only Down present, got %q", up)
	}
	if !strings.Contains(down, "DROP COLUMN") {
		t.Errorf("down: missing body, got %q", down)
	}
}

func TestSplitUpDown_TolerantWhitespace(t *testing.T) {
	// Markers must work despite leading/trailing whitespace, mixed casing
	// on the direction, and extra spaces inside the marker.
	const sql = `   --   +migrate    UP
SELECT 1;
--+migrate down
SELECT 2;
`

	up, down := splitUpDown(sql)

	if !strings.Contains(up, "SELECT 1;") {
		t.Errorf("up missing SELECT 1: %q", up)
	}
	if strings.Contains(up, "SELECT 2;") {
		t.Errorf("up leaked SELECT 2: %q", up)
	}
	if !strings.Contains(down, "SELECT 2;") {
		t.Errorf("down missing SELECT 2: %q", down)
	}
}

func TestSplitUpDown_HeaderCommentsAboveFirstMarker_AreDiscarded(t *testing.T) {
	// File-level header comments live above the first marker. They must
	// NOT be passed to Exec — they're documentation, not SQL we want to
	// run twice or run as part of a section.
	const sql = `-- Migration: 20260101_add_bar
-- Description: blah
-- Created: 2026-01-01

-- +migrate Up
ALTER TABLE foo ADD COLUMN bar INT;
`

	up, _ := splitUpDown(sql)

	if strings.Contains(up, "Description:") {
		t.Errorf("up leaked header comment: %q", up)
	}
	if !strings.Contains(up, "ADD COLUMN bar") {
		t.Errorf("up missing body: %q", up)
	}
}

func TestIsMigrationMarker(t *testing.T) {
	tests := []struct {
		line string
		dir  string
		want bool
	}{
		{"-- +migrate Up", "Up", true},
		{"  --   +migrate   Up  ", "Up", true},
		{"-- +migrate up", "Up", true},
		{"--+migrate Down", "Down", true},
		{"-- +migrate Up", "Down", false},
		{"-- +migrate UpExtra", "Up", false}, // strict suffix
		{"ALTER TABLE foo;", "Up", false},    // not a comment
		{"-- regular comment", "Up", false},  // comment without marker
		{"-- +migrate", "Up", false},         // direction missing
		{"", "Up", false},
	}
	for _, tc := range tests {
		got := isMigrationMarker(tc.line, tc.dir)
		if got != tc.want {
			t.Errorf("isMigrationMarker(%q, %q) = %v; want %v", tc.line, tc.dir, got, tc.want)
		}
	}
}
