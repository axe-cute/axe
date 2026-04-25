// Seed script — populates the webtoon database with demo series + episodes.
// Run:  go run ./cmd/seed
// Idempotent: re-running clears existing series/episodes and recreates them.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/axe-cute/examples-webtoon/config"
	ent "github.com/axe-cute/examples-webtoon/ent"
)

type seedEpisode struct {
	Title        string
	Number       int64
	ThumbnailURL string
}

type seedSeries struct {
	Title       string
	Description string
	Genre       string
	Author      string
	CoverURL    string
	Status      string
	Episodes    []seedEpisode
}

// Covers use Unsplash "source" URLs — deterministic by keyword, no API key.
// NOTE: replace with licensed art for real production deployments.
func coverURL(seed string) string {
	return fmt.Sprintf("https://picsum.photos/seed/%s/600/800", seed)
}

func thumbURL(seed string) string {
	return fmt.Sprintf("https://picsum.photos/seed/%s/400/240", seed)
}

func data() []seedSeries {
	return []seedSeries{
		{
			Title:       "Solo Ascension",
			Description: "Ten years after portals tore open the sky, the weakest hunter on Earth awakens in a Dungeon with a single message: Level Up. A dark fantasy about growth, loneliness, and the cost of power.",
			Genre:       "action", Author: "J. W. Park", Status: "ongoing",
			CoverURL: coverURL("solo-ascension"),
			Episodes: []seedEpisode{
				{Title: "The Weakest Hunter", Number: 1, ThumbnailURL: thumbURL("sa-1")},
				{Title: "System Awakening", Number: 2, ThumbnailURL: thumbURL("sa-2")},
				{Title: "First Gate", Number: 3, ThumbnailURL: thumbURL("sa-3")},
				{Title: "Red Gate", Number: 4, ThumbnailURL: thumbURL("sa-4")},
				{Title: "Shadow Soldier", Number: 5, ThumbnailURL: thumbURL("sa-5")},
			},
		},
		{
			Title:       "Coffee & Critique",
			Description: "A film critic moves to a small town and opens a café staffed entirely by fictional characters he's reviewed. Slice-of-life romance with a literary twist.",
			Genre:       "slice-of-life", Author: "M. Aizawa", Status: "ongoing",
			CoverURL: coverURL("coffee-critique"),
			Episodes: []seedEpisode{
				{Title: "Opening Day", Number: 1, ThumbnailURL: thumbURL("cc-1")},
				{Title: "The Regular", Number: 2, ThumbnailURL: thumbURL("cc-2")},
				{Title: "Sunday Rain", Number: 3, ThumbnailURL: thumbURL("cc-3")},
			},
		},
		{
			Title:       "Orbital",
			Description: "In 2147, humanity lives in rotating rings above a dying Earth. When a maintenance engineer finds an unlogged corridor, she uncovers why the founders never came back.",
			Genre:       "sci-fi", Author: "R. Oyelaran", Status: "ongoing",
			CoverURL: coverURL("orbital"),
			Episodes: []seedEpisode{
				{Title: "Ring 7", Number: 1, ThumbnailURL: thumbURL("or-1")},
				{Title: "The Unlogged Door", Number: 2, ThumbnailURL: thumbURL("or-2")},
				{Title: "Descent", Number: 3, ThumbnailURL: thumbURL("or-3")},
				{Title: "Earthfall", Number: 4, ThumbnailURL: thumbURL("or-4")},
			},
		},
		{
			Title:       "Thorn & Ember",
			Description: "The last princess of a fallen kingdom crosses the continent disguised as a mercenary, hunting the seven generals who burned her home.",
			Genre:       "fantasy", Author: "L. Castellani", Status: "completed",
			CoverURL: coverURL("thorn-ember"),
			Episodes: []seedEpisode{
				{Title: "Ash Road", Number: 1, ThumbnailURL: thumbURL("te-1")},
				{Title: "First General", Number: 2, ThumbnailURL: thumbURL("te-2")},
				{Title: "The Inn at Duskwater", Number: 3, ThumbnailURL: thumbURL("te-3")},
				{Title: "Silver Coin", Number: 4, ThumbnailURL: thumbURL("te-4")},
				{Title: "Return", Number: 5, ThumbnailURL: thumbURL("te-5")},
			},
		},
		{
			Title:       "Nightline",
			Description: "A true-crime podcaster starts receiving voicemails from listeners who went missing decades ago. Psychological horror at the edge of radio static.",
			Genre:       "horror", Author: "S. Deluca", Status: "ongoing",
			CoverURL: coverURL("nightline"),
			Episodes: []seedEpisode{
				{Title: "Episode 001: The Caller", Number: 1, ThumbnailURL: thumbURL("nl-1")},
				{Title: "Episode 002: Wrong Number", Number: 2, ThumbnailURL: thumbURL("nl-2")},
				{Title: "Episode 003: Static", Number: 3, ThumbnailURL: thumbURL("nl-3")},
			},
		},
		{
			Title:       "After Lunch",
			Description: "Two office workers have exactly 47 minutes of lunch break to solve whatever absurd crisis hits the city that day. Action-comedy in business-casual.",
			Genre:       "comedy", Author: "K. Tanaka", Status: "ongoing",
			CoverURL: coverURL("after-lunch"),
			Episodes: []seedEpisode{
				{Title: "Bank Robbery on Tuesday", Number: 1, ThumbnailURL: thumbURL("al-1")},
				{Title: "The Copier Is a Portal", Number: 2, ThumbnailURL: thumbURL("al-2")},
				{Title: "HR Ghosts", Number: 3, ThumbnailURL: thumbURL("al-3")},
				{Title: "Quarterly Review", Number: 4, ThumbnailURL: thumbURL("al-4")},
			},
		},
		{
			Title:       "Pitch Perfect Kick",
			Description: "A high-schooler who can calculate the exact angle of any kick but can't run for ten minutes joins her school's broken football team.",
			Genre:       "sports", Author: "Y. Ibarra", Status: "ongoing",
			CoverURL: coverURL("pitch-kick"),
			Episodes: []seedEpisode{
				{Title: "First Touch", Number: 1, ThumbnailURL: thumbURL("pk-1")},
				{Title: "The Bench Eleven", Number: 2, ThumbnailURL: thumbURL("pk-2")},
				{Title: "Rival School", Number: 3, ThumbnailURL: thumbURL("pk-3")},
			},
		},
		{
			Title:       "The Letter We Didn't Send",
			Description: "Two childhood friends reunite at twenty-eight to return a letter they never delivered. A quiet romance told across one city, one week, and six cafés.",
			Genre:       "romance", Author: "A. Nguyen", Status: "ongoing",
			CoverURL: coverURL("letter-unsent"),
			Episodes: []seedEpisode{
				{Title: "Monday Morning", Number: 1, ThumbnailURL: thumbURL("lu-1")},
				{Title: "The First Café", Number: 2, ThumbnailURL: thumbURL("lu-2")},
				{Title: "Under the Overpass", Number: 3, ThumbnailURL: thumbURL("lu-3")},
				{Title: "Sunday", Number: 4, ThumbnailURL: thumbURL("lu-4")},
			},
		},
	}
}

func main() {
	cfg, err := config.LoadFromFile(".env")
	if err != nil {
		cfg, err = config.Load()
		if err != nil {
			log.Fatalf("seed: load config: %v", err)
		}
	}

	ctx := context.Background()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("seed: open db: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("seed: ping db: %v", err)
	}

	drv := entsql.OpenDB("postgres", db)
	client := ent.NewClient(ent.Driver(drv))
	defer client.Close()

	log.Println("seed: clearing existing demo data...")
	if _, err := client.Episode.Delete().Exec(ctx); err != nil {
		log.Fatalf("seed: delete episodes: %v", err)
	}
	if _, err := client.Series.Delete().Exec(ctx); err != nil {
		log.Fatalf("seed: delete series: %v", err)
	}
	if _, err := client.Bookmark.Delete().Exec(ctx); err != nil {
		log.Fatalf("seed: delete bookmarks: %v", err)
	}

	for _, s := range data() {
		created, err := client.Series.Create().
			SetTitle(s.Title).
			SetDescription(s.Description).
			SetGenre(s.Genre).
			SetAuthor(s.Author).
			SetCoverURL(s.CoverURL).
			SetStatus(s.Status).
			Save(ctx)
		if err != nil {
			log.Fatalf("seed: create series %q: %v", s.Title, err)
		}
		log.Printf("seed: + series %q (%s)", s.Title, created.ID)

		for _, ep := range s.Episodes {
			_, err := client.Episode.Create().
				SetTitle(ep.Title).
				SetEpisodeNumber(ep.Number).
				SetThumbnailURL(ep.ThumbnailURL).
				SetPublished(true).
				SetSeriesID(created.ID).
				Save(ctx)
			if err != nil {
				log.Fatalf("seed: create episode %q: %v", ep.Title, err)
			}
		}
		log.Printf("  └─ %d episodes", len(s.Episodes))
	}

	log.Printf("seed: done in %s", time.Since(time.Now().Add(0))) // placeholder timing
	_, _ = fmt.Fprintln(os.Stdout, "✅ seed complete")
}
