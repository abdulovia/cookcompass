package ingest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// JSONLDSource scrapes schema.org Recipe JSON-LD from a list of pages.
type JSONLDSource struct {
	Fetcher *fetch.Client
	Targets []string
	Log     *slog.Logger
}

// Name implements recipes.RecipeSource.
func (s *JSONLDSource) Name() string { return "jsonld" }

// Fetch implements recipes.RecipeSource. Unreachable or blocked pages are
// logged and skipped; the error is non-nil only when nothing at all could
// be read, so an offline run degrades to "no new recipes".
func (s *JSONLDSource) Fetch(ctx context.Context, opts recipes.FetchOpts) ([]recipes.RawRecipe, error) {
	targets := s.Targets
	if len(opts.URLs) > 0 {
		targets = opts.URLs
	}
	var out []recipes.RawRecipe
	var errs []error
	for _, u := range targets {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		body, err := s.Fetcher.Get(ctx, u)
		if err != nil {
			errs = append(errs, err)
			if s.Log != nil {
				s.Log.Warn("skip page", "url", u, "err", err)
			}
			continue
		}
		found := ExtractJSONLD(body, u)
		if len(found) == 0 && s.Log != nil {
			s.Log.Info("no recipe JSON-LD on page", "url", u)
		}
		for _, r := range found {
			out = append(out, r)
			if opts.Limit > 0 && len(out) >= opts.Limit {
				return out, nil
			}
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ReadTargets parses a targets file: one URL per line, "#" comments.
// "file:" entries may use glob patterns and are resolved relative to the
// file's directory when not absolute.
func ReadTargets(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "file:") {
			pattern := strings.TrimPrefix(strings.TrimPrefix(line, "file:"), "//")
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(filepath.Dir(path), pattern)
			}
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", line, err)
			}
			sort.Strings(matches)
			for _, m := range matches {
				out = append(out, "file:"+m)
			}
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}

// Result summarizes an import run.
type Result struct {
	Fetched    int
	Created    int
	Updated    int
	Withheld   int // re-scrapes of recipes a user has edited: recorded, not applied
	Unchanged  int
	Invalid    int
	PendingIDs []int64 // recipes left in the queue by this run (for targeted approval)
	Errors     []error
}

func (r Result) String() string {
	return fmt.Sprintf("fetched=%d created=%d updated=%d withheld=%d unchanged=%d invalid=%d",
		r.Fetched, r.Created, r.Updated, r.Withheld, r.Unchanged, r.Invalid)
}

// Importer normalizes raw recipes and stores them in the review queue.
type Importer struct {
	Recipes  *recipes.Store
	Cuisines map[string]string // lowercased name/alias → cuisine slug
	Log      *slog.Logger
}

// NewImporter builds an Importer with the cuisine table from the database.
func NewImporter(ctx context.Context, store *recipes.Store, log *slog.Logger) (*Importer, error) {
	cs, err := store.Cuisines(ctx)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, c := range cs {
		m[strings.ToLower(c.Name)] = c.Slug
		m[strings.ReplaceAll(c.Slug, "-", " ")] = c.Slug
	}
	for alias, slug := range cuisineAliases {
		if _, ok := m[strings.ReplaceAll(slug, "-", " ")]; ok {
			m[alias] = slug
		}
	}
	return &Importer{Recipes: store, Cuisines: m, Log: log}, nil
}

var cuisineAliases = map[string]string{
	"lebanese": "middle-eastern", "israeli": "middle-eastern", "turkish": "middle-eastern", "persian": "middle-eastern",
	"ukrainian": "russian", "eastern european": "russian", "english": "british", "scottish": "british", "irish": "british",
	"southern": "american", "tex-mex": "mexican", "cajun": "american", "szechuan": "chinese", "sichuan": "chinese",
	"cantonese": "chinese", "vietnamese": "thai", "mediterranean": "greek", "punjabi": "indian", "korean bbq": "korean",
}

// Import normalizes and stores raws, deduplicating against existing rows.
func (im *Importer) Import(ctx context.Context, raws []recipes.RawRecipe) Result {
	res := Result{Fetched: len(raws)}
	for _, raw := range raws {
		r, err := im.Normalize(raw)
		if err != nil {
			res.Invalid++
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", raw.URL, err))
			continue
		}
		existing, err := im.Recipes.FindDuplicate(ctx, r.CanonicalURL, recipes.RecipeContentKey(r))
		if err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		ch := recipes.Change{Source: r.Source, Note: "scraped from " + raw.URL}
		switch {
		case existing == nil:
			err = im.Recipes.Create(ctx, r, ch)
			if err == nil {
				res.Created++
				res.PendingIDs = append(res.PendingIDs, r.ID)
			}
		case existing.Source == recipes.SourceUser:
			ch.Note = "scrape withheld: user edits are authoritative"
			err = im.Recipes.RecordWithheld(ctx, existing.ID, r, ch)
			if err == nil {
				res.Withheld++
			}
		default:
			r.ID, r.Slug, r.Status = existing.ID, existing.Slug, existing.Status
			for loc, t := range existing.Translations {
				if loc == recipes.BaseLocale {
					continue
				}
				if r.Translations == nil {
					r.Translations = map[string]recipes.Translation{}
				}
				r.Translations[loc] = t
			}
			if sameContent(existing, r) {
				res.Unchanged++
				if existing.Status == recipes.StatusPending {
					res.PendingIDs = append(res.PendingIDs, existing.ID)
				}
				continue
			}
			ch.Note = "re-scraped from " + raw.URL
			err = im.Recipes.Update(ctx, r, ch)
			if err == nil {
				res.Updated++
				if r.Status == recipes.StatusPending {
					res.PendingIDs = append(res.PendingIDs, r.ID)
				}
			}
		}
		if err != nil {
			var ve recipes.ValidationError
			if errors.As(err, &ve) {
				res.Invalid++
			}
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", raw.URL, err))
		}
	}
	if im.Log != nil {
		im.Log.Info("import finished", "result", res.String())
	}
	return res
}

// sameContent compares the parts of a recipe a scrape can change.
func sameContent(a, b *recipes.Recipe) bool {
	if a.Name != b.Name || a.Description != b.Description || a.Cuisine != b.Cuisine || a.Difficulty != b.Difficulty ||
		a.PrepMinutes != b.PrepMinutes || a.CookMinutes != b.CookMinutes || a.Servings != b.Servings ||
		a.VideoURL != b.VideoURL || a.ImageURL != b.ImageURL || a.Nutrition != b.Nutrition ||
		strings.Join(a.Steps, "\n") != strings.Join(b.Steps, "\n") || strings.Join(a.MealTimes, ",") != strings.Join(b.MealTimes, ",") ||
		len(a.Ingredients) != len(b.Ingredients) {
		return false
	}
	for i := range a.Ingredients {
		if a.Ingredients[i].Raw != b.Ingredients[i].Raw {
			return false
		}
	}
	return true
}

var mealKeywords = map[string][]string{
	recipes.Breakfast: {"breakfast", "brunch", "pancake", "omelet", "omelette", "porridge", "oatmeal", "granola", "waffle", "french toast", "завтрак"},
	recipes.Snack:     {"snack", "appetizer", "starter", "dip", "side dish", "finger food", "закуска"},
	recipes.Lunch:     {"lunch", "salad", "soup", "sandwich", "обед", "суп", "салат"},
	recipes.Dinner:    {"dinner", "main course", "main dish", "entree", "supper", "ужин"},
}

var reFirstInt = regexp.MustCompile(`\d+`)

// Normalize maps a raw recipe onto the domain model for the review queue.
func (im *Importer) Normalize(raw recipes.RawRecipe) (*recipes.Recipe, error) {
	if strings.TrimSpace(raw.Name) == "" {
		return nil, errors.New("recipe has no name")
	}
	if len(raw.Ingredients) == 0 || len(raw.Steps) == 0 {
		return nil, errors.New("recipe has no ingredients or steps")
	}
	domain := recipes.Domain(firstNonEmpty(raw.CanonicalURL, raw.URL))
	if domain == "" {
		domain = "local"
	}
	r := &recipes.Recipe{
		Name:         raw.Name,
		Description:  truncate(raw.Description, 500),
		Source:       recipes.ScrapedSource(domain),
		SourceURL:    raw.URL,
		CanonicalURL: raw.CanonicalURL,
		Status:       recipes.StatusPending,
		Steps:        raw.Steps,
		PrepMinutes:  raw.PrepMinutes,
		CookMinutes:  raw.CookMinutes,
	}
	if r.PrepMinutes == 0 && r.CookMinutes == 0 && raw.TotalMinutes > 0 {
		r.CookMinutes = raw.TotalMinutes
	}
	if recipes.IsYouTubeURL(raw.VideoURL) {
		r.VideoURL = raw.VideoURL
	}
	if strings.HasPrefix(raw.ImageURL, "http://") || strings.HasPrefix(raw.ImageURL, "https://") {
		r.ImageURL = raw.ImageURL
	}
	if raw.Nutrition != nil {
		r.Nutrition = *raw.Nutrition
	}
	r.Servings = 2
	if n, err := strconv.Atoi(reFirstInt.FindString(raw.Yield)); err == nil && n > 0 && n <= 100 {
		r.Servings = n
	}
	for _, c := range raw.Cuisines {
		if slug, ok := im.Cuisines[strings.ToLower(strings.TrimSpace(c))]; ok {
			r.Cuisine = slug
			break
		}
	}
	r.MealTimes = guessMeals(raw)
	for i, line := range raw.Ingredients {
		p := recipes.ParseIngredient(line)
		name := p.Name
		if name == "" {
			name = line
		}
		r.Ingredients = append(r.Ingredients, recipes.Ingredient{Position: i, Raw: line, Quantity: p.Quantity, Unit: p.Unit,
			Name: name, Note: p.Note, Essential: !isOptional(line, p), NeedsReview: p.NeedsReview})
	}
	r.Difficulty = guessDifficulty(r)
	return r, nil
}

func isOptional(line string, p recipes.ParsedIngredient) bool {
	l := strings.ToLower(line + " " + p.Note)
	return strings.Contains(l, "optional") || strings.Contains(l, "for garnish") || strings.Contains(l, "for serving") ||
		strings.Contains(l, "по желанию") || strings.Contains(l, "для подачи")
}

func guessMeals(raw recipes.RawRecipe) []string {
	text := strings.ToLower(strings.Join(append(append(append([]string{raw.Name}, raw.Categories...), raw.Keywords...), raw.Cuisines...), " | "))
	var out []string
	for _, meal := range recipes.MealTimes {
		for _, kw := range mealKeywords[meal] {
			if strings.Contains(text, kw) {
				out = append(out, meal)
				break
			}
		}
	}
	if len(out) == 0 {
		out = []string{recipes.Dinner}
	}
	return out
}

// guessDifficulty is a heuristic from time, step count and ingredient count;
// reviewers can correct it in the admin form.
func guessDifficulty(r *recipes.Recipe) string {
	total, steps, ings := r.TotalMinutes(), len(r.Steps), len(r.Ingredients)
	switch {
	case total > 90 || steps > 10 || ings > 14:
		return recipes.Hard
	case total <= 30 && steps <= 6 && ings <= 8:
		return recipes.Easy
	default:
		return recipes.Medium
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// Sources lists the available recipe sources by name. Add new sources here
// (see docs/ingestion.md).
func Sources(fetcher *fetch.Client, targetsFile, datasetInbox string, log *slog.Logger) (map[string]recipes.RecipeSource, error) {
	targets, err := ReadTargets(targetsFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err != nil && log != nil {
		log.Warn("targets file not found; only --url targets will be scraped", "file", targetsFile)
	}
	return map[string]recipes.RecipeSource{
		"jsonld":  &JSONLDSource{Fetcher: fetcher, Targets: targets, Log: log},
		"dataset": &DatasetSource{Fetcher: fetcher, Dir: datasetInbox, Log: log},
	}, nil
}
