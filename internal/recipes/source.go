package recipes

import "context"

// RecipeSource produces raw recipes from somewhere outside the database:
// a website, a file dump, an API. Implementations must be polite (respect
// robots.txt and rate limits) and must not fail the whole run because one
// page is broken — they return what they could read plus an error
// describing what they couldn't.
//
// Register new sources in internal/ingest (see docs/ingestion.md).
type RecipeSource interface {
	Name() string
	Fetch(ctx context.Context, opts FetchOpts) ([]RawRecipe, error)
}

// FetchOpts narrows a fetch run.
type FetchOpts struct {
	// URLs overrides the source's configured targets when non-empty.
	URLs []string
	// Limit caps the number of recipes returned (0 = no limit).
	Limit int
}

// RawRecipe is a recipe as found in the wild, before normalization.
type RawRecipe struct {
	Source       string     `json:"source"`        // source name, e.g. "jsonld"
	URL          string     `json:"url"`           // page it came from
	CanonicalURL string     `json:"canonical_url"` // declared canonical URL, if any
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	Ingredients  []string   `json:"ingredients"` // free-text lines
	Steps        []string   `json:"steps"`
	Cuisines     []string   `json:"cuisines,omitempty"`
	Categories   []string   `json:"categories,omitempty"`
	Keywords     []string   `json:"keywords,omitempty"`
	PrepMinutes  int        `json:"prep_minutes,omitempty"`
	CookMinutes  int        `json:"cook_minutes,omitempty"`
	TotalMinutes int        `json:"total_minutes,omitempty"`
	Yield        string     `json:"yield,omitempty"`
	VideoURL     string     `json:"video_url,omitempty"`
	ImageURL     string     `json:"image_url,omitempty"`
	Nutrition    *Nutrition `json:"nutrition,omitempty"`
	Language     string     `json:"language,omitempty"`
}
