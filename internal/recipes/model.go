// Package recipes holds the recipe domain model, persistence with
// append-only revisions, ingredient-line parsing and deduplication.
package recipes

import (
	"net/url"
	"strings"
	"time"
)

// Difficulty levels.
const (
	Easy   = "easy"
	Medium = "medium"
	Hard   = "hard"
)

// Difficulties lists valid difficulty values in learning order.
var Difficulties = []string{Easy, Medium, Hard}

// Meal times.
const (
	Breakfast = "breakfast"
	Lunch     = "lunch"
	Dinner    = "dinner"
	Snack     = "snack"
)

// MealTimes lists valid meal times in day order.
var MealTimes = []string{Breakfast, Lunch, Dinner, Snack}

// Statuses.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Sources. Scraped recipes use "scraped:<domain>".
const (
	SourceSeed    = "seed"
	SourceUser    = "user"
	SourceScraped = "scraped"
)

// ScrapedSource returns the provenance tag for a scraped domain.
func ScrapedSource(domain string) string { return SourceScraped + ":" + domain }

// Nutrition is per serving. Sodium is in milligrams, everything else in
// grams except Calories (kcal).
type Nutrition struct {
	Calories     float64 `json:"calories"`
	Protein      float64 `json:"protein"`
	Fat          float64 `json:"fat"`
	Carbs        float64 `json:"carbs"`
	Fiber        float64 `json:"fiber"`
	Sugar        float64 `json:"sugar"`
	Sodium       float64 `json:"sodium"`
	SaturatedFat float64 `json:"saturated_fat"`
}

// Scale multiplies every value by f.
func (n Nutrition) Scale(f float64) Nutrition {
	return Nutrition{n.Calories * f, n.Protein * f, n.Fat * f, n.Carbs * f, n.Fiber * f, n.Sugar * f, n.Sodium * f, n.SaturatedFat * f}
}

// Add sums two nutrition values.
func (n Nutrition) Add(o Nutrition) Nutrition {
	return Nutrition{n.Calories + o.Calories, n.Protein + o.Protein, n.Fat + o.Fat, n.Carbs + o.Carbs,
		n.Fiber + o.Fiber, n.Sugar + o.Sugar, n.Sodium + o.Sodium, n.SaturatedFat + o.SaturatedFat}
}

// Ingredient is one line of a recipe's ingredient list.
type Ingredient struct {
	Position     int      `json:"position"`
	IngredientID int64    `json:"ingredient_id,omitempty"` // 0 when not mapped to a canonical ingredient
	Raw          string   `json:"raw,omitempty"`
	Quantity     *float64 `json:"quantity,omitempty"`
	Unit         string   `json:"unit,omitempty"`
	Name         string   `json:"name"`
	Note         string   `json:"note,omitempty"`
	Essential    bool     `json:"essential"`
	NeedsReview  bool     `json:"needs_review,omitempty"`
}

// Translation is localized recipe text.
type Translation struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Steps       []string `json:"steps,omitempty"`
	Ingredients []string `json:"ingredients,omitempty"` // display lines aligned with Recipe.Ingredients
}

// Recipe is the aggregate root.
type Recipe struct {
	ID           int64                  `json:"id"`
	Slug         string                 `json:"slug"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	CuisineID    int64                  `json:"cuisine_id,omitempty"`
	Cuisine      string                 `json:"cuisine,omitempty"` // slug
	MealTimes    []string               `json:"meal_times"`
	Difficulty   string                 `json:"difficulty"`
	PrepMinutes  int                    `json:"prep_minutes"`
	CookMinutes  int                    `json:"cook_minutes"`
	Servings     int                    `json:"servings"`
	VideoURL     string                 `json:"video_url,omitempty"`
	ImageURL     string                 `json:"image_url,omitempty"`
	Steps        []string               `json:"steps"`
	Nutrition    Nutrition              `json:"nutrition"`
	Source       string                 `json:"source"`
	SourceURL    string                 `json:"source_url,omitempty"`
	CanonicalURL string                 `json:"canonical_url,omitempty"`
	DedupKey     string                 `json:"dedup_key,omitempty"`
	Status       string                 `json:"status"`
	NeedsReview  bool                   `json:"needs_review,omitempty"`
	Ingredients  []Ingredient           `json:"ingredients"`
	Translations map[string]Translation `json:"translations,omitempty"`
	CreatedAt    time.Time              `json:"-"`
	UpdatedAt    time.Time              `json:"-"`
	DeletedAt    *time.Time             `json:"-"`
}

// TotalMinutes is prep plus cook time.
func (r *Recipe) TotalMinutes() int { return r.PrepMinutes + r.CookMinutes }

// Deleted reports whether the recipe is soft-deleted.
func (r *Recipe) Deleted() bool { return r.DeletedAt != nil }

// HasMealTime reports whether the recipe suits meal time m.
func (r *Recipe) HasMealTime(m string) bool {
	for _, x := range r.MealTimes {
		if x == m {
			return true
		}
	}
	return false
}

// IsScraped reports whether the recipe came from a scraper.
func (r *Recipe) IsScraped() bool { return strings.HasPrefix(r.Source, SourceScraped+":") }

// Localized returns a shallow copy with text fields replaced by the given
// locale's translation when present. Missing pieces fall back to the base.
func (r *Recipe) Localized(locale string) *Recipe {
	c := *r
	t, ok := r.Translations[locale]
	if !ok {
		return &c
	}
	if t.Name != "" {
		c.Name = t.Name
	}
	if t.Description != "" {
		c.Description = t.Description
	}
	if len(t.Steps) > 0 {
		c.Steps = t.Steps
	}
	return &c
}

// IngredientLine returns the localized display line for ingredient i, or ""
// when no translation exists for it.
func (r *Recipe) IngredientLine(locale string, i int) string {
	t, ok := r.Translations[locale]
	if !ok || i < 0 || i >= len(t.Ingredients) {
		return ""
	}
	return t.Ingredients[i]
}

// YouTubeID extracts a video id from common YouTube URL shapes.
func YouTubeID(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	host = strings.TrimPrefix(host, "m.")
	var id string
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/embed/"), strings.HasPrefix(u.Path, "/shorts/"), strings.HasPrefix(u.Path, "/v/"):
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 {
				id = parts[1]
			}
		}
	}
	if len(id) != 11 {
		return "", false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return "", false
		}
	}
	return id, true
}

// EmbedURL returns a privacy-friendly embed URL for a YouTube link.
func EmbedURL(raw string) (string, bool) {
	id, ok := YouTubeID(raw)
	if !ok {
		return "", false
	}
	return "https://www.youtube-nocookie.com/embed/" + id, true
}

// IsYouTubeURL reports whether raw points at YouTube (any page, including
// search results used as a fallback when no specific video is known).
func IsYouTubeURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	h := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	h = strings.TrimPrefix(h, "m.")
	return h == "youtube.com" || h == "youtu.be" || h == "youtube-nocookie.com"
}
