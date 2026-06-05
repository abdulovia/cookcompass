package recipes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/abdulovia/cookcompass/internal/db"
	"github.com/abdulovia/cookcompass/internal/ingredients"
)

// ErrNotFound is returned when a recipe does not exist.
var ErrNotFound = errors.New("recipe not found")

// Change describes who/what caused a write; it is recorded in the revision.
type Change struct {
	Source string // seed | user | scraped:<domain> | cli
	Note   string
}

// Revision is an immutable snapshot of a recipe.
type Revision struct {
	ID        int64
	RecipeID  int64
	Rev       int
	Source    string
	Note      string
	Applied   bool
	Snapshot  string
	CreatedAt time.Time
}

// Cuisine is a world cuisine.
type Cuisine struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// DeletedMode controls soft-deleted rows in List.
type DeletedMode int

// Deleted modes.
const (
	DeletedExclude DeletedMode = iota
	DeletedInclude
	DeletedOnly
)

// Filter narrows List results. Zero value = approved, not deleted.
type Filter struct {
	Statuses             []string
	Deleted              DeletedMode
	Cuisine              string
	MealTime             string
	Difficulty           string
	Query                string
	Source               string // exact, or prefix when ending in ':'
	ExcludeIngredientIDs []int64
	IDs                  []int64
	NeedsReview          bool
	Limit                int
	Offset               int
}

// Store persists recipes.
type Store struct {
	db   *sql.DB
	ings *ingredients.Store
	keep int
}

// NewStore returns a Store keeping the last keep revisions per recipe.
func NewStore(d *sql.DB, ings *ingredients.Store, keep int) *Store {
	if keep < 1 {
		keep = 20
	}
	return &Store{db: d, ings: ings, keep: keep}
}

// UpsertCuisine creates or renames a cuisine by slug.
func (s *Store) UpsertCuisine(ctx context.Context, slug, name string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO cuisines (slug, name) VALUES (?, ?)
		ON CONFLICT (slug) DO UPDATE SET name = excluded.name RETURNING id`, slug, name).Scan(&id)
	return id, err
}

// Cuisines lists all cuisines by name.
func (s *Store) Cuisines(ctx context.Context) ([]Cuisine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name FROM cuisines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cuisine
	for rows.Next() {
		var c Cuisine
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// prepare validates r, resolves cuisine and ingredient ids, and computes
// derived fields.
func (s *Store) prepare(ctx context.Context, r *Recipe) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if r.Cuisine != "" {
		err := s.db.QueryRowContext(ctx, `SELECT id FROM cuisines WHERE slug = ?`, r.Cuisine).Scan(&r.CuisineID)
		if errors.Is(err, sql.ErrNoRows) {
			return ValidationError{"cuisine": "validation.invalid_choice"}
		}
		if err != nil {
			return err
		}
	} else {
		r.CuisineID = 0
	}
	if s.ings != nil {
		m, err := s.ings.Matcher(ctx)
		if err != nil {
			return err
		}
		for i := range r.Ingredients {
			if r.Ingredients[i].IngredientID == 0 {
				if id, ok := m.Match(r.Ingredients[i].Name); ok {
					r.Ingredients[i].IngredientID = id
				}
			}
		}
	}
	// The base-locale translation always mirrors the recipe's own text.
	if _, ok := r.Translations[BaseLocale]; ok {
		t := r.Translations[BaseLocale]
		t.Name, t.Description, t.Steps = r.Name, r.Description, r.Steps
		r.Translations[BaseLocale] = t
	}
	r.NeedsReview = false
	for _, in := range r.Ingredients {
		if in.NeedsReview {
			r.NeedsReview = true
		}
	}
	if r.Status == "" {
		r.Status = StatusApproved
	}
	if r.Source == "" {
		r.Source = SourceUser
	}
	r.CanonicalURL = CanonicalizeURL(r.CanonicalURL)
	r.DedupKey = RecipeContentKey(r)
	return nil
}

// Create inserts a new recipe and its first revision. r.ID and r.Slug are set.
func (s *Store) Create(ctx context.Context, r *Recipe, ch Change) error {
	if err := s.prepare(ctx, r); err != nil {
		return err
	}
	return db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		slug, err := uniqueSlug(ctx, tx, firstNonEmpty(r.Slug, Slugify(r.Name)), 0)
		if err != nil {
			return err
		}
		r.Slug = slug
		steps, _ := json.Marshal(r.Steps)
		now := db.Now()
		err = tx.QueryRowContext(ctx, `INSERT INTO recipes (slug, name, description, cuisine_id, meal_times, difficulty,
			prep_minutes, cook_minutes, servings, video_url, image_url, steps_json, calories, protein, fat, carbs, fiber,
			sugar, sodium, saturated_fat, source, source_url, canonical_url, dedup_key, status, needs_review, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
			r.Slug, r.Name, r.Description, nullID(r.CuisineID), strings.Join(r.MealTimes, ","), r.Difficulty,
			r.PrepMinutes, r.CookMinutes, r.Servings, r.VideoURL, r.ImageURL, string(steps),
			r.Nutrition.Calories, r.Nutrition.Protein, r.Nutrition.Fat, r.Nutrition.Carbs, r.Nutrition.Fiber,
			r.Nutrition.Sugar, r.Nutrition.Sodium, r.Nutrition.SaturatedFat, r.Source, r.SourceURL,
			nullStr(r.CanonicalURL), r.DedupKey, r.Status, r.NeedsReview, now, now).Scan(&r.ID)
		if err != nil {
			return fmt.Errorf("insert recipe: %w", err)
		}
		r.CreatedAt, r.UpdatedAt = db.ParseTime(now), db.ParseTime(now)
		if err := writeChildren(ctx, tx, r); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, r, ch, true)
	})
}

// Update replaces a recipe's content and records a revision.
func (s *Store) Update(ctx context.Context, r *Recipe, ch Change) error {
	if r.ID == 0 {
		return ErrNotFound
	}
	if err := s.prepare(ctx, r); err != nil {
		return err
	}
	return db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		slug, err := uniqueSlug(ctx, tx, firstNonEmpty(r.Slug, Slugify(r.Name)), r.ID)
		if err != nil {
			return err
		}
		r.Slug = slug
		steps, _ := json.Marshal(r.Steps)
		now := db.Now()
		res, err := tx.ExecContext(ctx, `UPDATE recipes SET slug=?, name=?, description=?, cuisine_id=?, meal_times=?,
			difficulty=?, prep_minutes=?, cook_minutes=?, servings=?, video_url=?, image_url=?, steps_json=?,
			calories=?, protein=?, fat=?, carbs=?, fiber=?, sugar=?, sodium=?, saturated_fat=?, source=?, source_url=?,
			canonical_url=?, dedup_key=?, status=?, needs_review=?, updated_at=? WHERE id=?`,
			r.Slug, r.Name, r.Description, nullID(r.CuisineID), strings.Join(r.MealTimes, ","), r.Difficulty,
			r.PrepMinutes, r.CookMinutes, r.Servings, r.VideoURL, r.ImageURL, string(steps),
			r.Nutrition.Calories, r.Nutrition.Protein, r.Nutrition.Fat, r.Nutrition.Carbs, r.Nutrition.Fiber,
			r.Nutrition.Sugar, r.Nutrition.Sodium, r.Nutrition.SaturatedFat, r.Source, r.SourceURL,
			nullStr(r.CanonicalURL), r.DedupKey, r.Status, r.NeedsReview, now, r.ID)
		if err != nil {
			return fmt.Errorf("update recipe: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		r.UpdatedAt = db.ParseTime(now)
		for _, q := range []string{`DELETE FROM recipe_ingredients WHERE recipe_id = ?`, `DELETE FROM recipe_i18n WHERE recipe_id = ?`} {
			if _, err := tx.ExecContext(ctx, q, r.ID); err != nil {
				return err
			}
		}
		if err := writeChildren(ctx, tx, r); err != nil {
			return err
		}
		return s.addRevision(ctx, tx, r, ch, true)
	})
}

func writeChildren(ctx context.Context, tx *sql.Tx, r *Recipe) error {
	for i, in := range r.Ingredients {
		var qty any
		if in.Quantity != nil {
			qty = *in.Quantity
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_ingredients (recipe_id, position, ingredient_id, raw, quantity,
			unit, name, note, essential, needs_review) VALUES (?,?,?,?,?,?,?,?,?,?)`,
			r.ID, i, nullID(in.IngredientID), in.Raw, qty, in.Unit, in.Name, in.Note, in.Essential, in.NeedsReview); err != nil {
			return fmt.Errorf("insert ingredient: %w", err)
		}
	}
	locales := make([]string, 0, len(r.Translations))
	for loc := range r.Translations {
		locales = append(locales, loc)
	}
	sort.Strings(locales)
	for _, loc := range locales {
		t := r.Translations[loc]
		steps, _ := json.Marshal(nonNil(t.Steps))
		ings, _ := json.Marshal(nonNil(t.Ingredients))
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_i18n (recipe_id, locale, name, description, steps_json, ingredients_json)
			VALUES (?,?,?,?,?,?)`, r.ID, loc, t.Name, t.Description, string(steps), string(ings)); err != nil {
			return fmt.Errorf("insert translation: %w", err)
		}
	}
	return nil
}

// snapshot is the JSON stored in a revision.
type snapshot struct {
	*Recipe
	Deleted bool `json:"deleted,omitempty"`
}

// Snapshot renders the canonical revision JSON for r.
func Snapshot(r *Recipe) string {
	b, _ := json.MarshalIndent(snapshot{Recipe: r, Deleted: r.Deleted()}, "", "  ")
	return string(b)
}

func (s *Store) addRevision(ctx context.Context, tx *sql.Tx, r *Recipe, ch Change, applied bool) error {
	if ch.Source == "" {
		ch.Source = r.Source
	}
	var rev int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(rev), 0) + 1 FROM recipe_revisions WHERE recipe_id = ?`, r.ID).Scan(&rev); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_revisions (recipe_id, rev, source, note, applied, snapshot_json, created_at)
		VALUES (?,?,?,?,?,?,?)`, r.ID, rev, ch.Source, ch.Note, applied, Snapshot(r), db.Now()); err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM recipe_revisions WHERE recipe_id = ? AND rev <= ?`, r.ID, rev-s.keep)
	return err
}

// RecordWithheld stores a revision for incoming data that was not applied
// (e.g. a re-scrape of a recipe the user has edited).
func (s *Store) RecordWithheld(ctx context.Context, id int64, incoming *Recipe, ch Change) error {
	c := *incoming
	c.ID = id
	return db.Tx(ctx, s.db, func(tx *sql.Tx) error { return s.addRevision(ctx, tx, &c, ch, false) })
}

func (s *Store) mutate(ctx context.Context, id int64, ch Change, query string, args ...any) error {
	return db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		r, err := getTx(ctx, tx, id)
		if err != nil {
			return err
		}
		return s.addRevision(ctx, tx, r, ch, true)
	})
}

// SoftDelete hides a recipe without removing it (history keeps referencing it).
func (s *Store) SoftDelete(ctx context.Context, id int64, ch Change) error {
	if ch.Note == "" {
		ch.Note = "deleted"
	}
	now := db.Now()
	return s.mutate(ctx, id, ch, `UPDATE recipes SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, now, now, id)
}

// Restore undoes SoftDelete.
func (s *Store) Restore(ctx context.Context, id int64, ch Change) error {
	if ch.Note == "" {
		ch.Note = "restored"
	}
	return s.mutate(ctx, id, ch, `UPDATE recipes SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL`, db.Now(), id)
}

// SetStatus moves a recipe through the review workflow.
func (s *Store) SetStatus(ctx context.Context, id int64, status string, ch Change) error {
	if !oneOf(status, []string{StatusPending, StatusApproved, StatusRejected}) {
		return ValidationError{"status": "validation.invalid_choice"}
	}
	if ch.Note == "" {
		ch.Note = "status: " + status
	}
	return s.mutate(ctx, id, ch, `UPDATE recipes SET status = ?, updated_at = ? WHERE id = ? AND status <> ?`, status, db.Now(), id, status)
}

// SetImageURL points a recipe at (usually) the local /media proxy after its
// image was mirrored into storage. This is bookkeeping for a derived asset,
// so unlike content edits it records no revision row.
func (s *Store) SetImageURL(ctx context.Context, id int64, image string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE recipes SET image_url = ?, updated_at = ? WHERE id = ?`,
		image, db.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ApproveAllPending approves every pending recipe and returns the count.
func (s *Store) ApproveAllPending(ctx context.Context, ch Change) (int, error) {
	list, err := s.List(ctx, Filter{Statuses: []string{StatusPending}, Deleted: DeletedInclude})
	if err != nil {
		return 0, err
	}
	for _, r := range list {
		if err := s.SetStatus(ctx, r.ID, StatusApproved, ch); err != nil {
			return 0, err
		}
	}
	return len(list), nil
}

// PurgePending permanently removes recipes still waiting for review. They
// were never visible, so no user history can reference them.
func (s *Store) PurgePending(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM recipes WHERE status = 'pending'
		AND id NOT IN (SELECT recipe_id FROM cooked_log) AND id NOT IN (SELECT recipe_id FROM learned)`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

const recipeCols = `r.id, r.slug, r.name, r.description, COALESCE(r.cuisine_id, 0), COALESCE(c.slug, ''), r.meal_times,
	r.difficulty, r.prep_minutes, r.cook_minutes, r.servings, r.video_url, r.image_url, r.steps_json, r.calories,
	r.protein, r.fat, r.carbs, r.fiber, r.sugar, r.sodium, r.saturated_fat, r.source, r.source_url,
	COALESCE(r.canonical_url, ''), r.dedup_key, r.status, r.needs_review, r.created_at, r.updated_at, r.deleted_at
	FROM recipes r LEFT JOIN cuisines c ON c.id = r.cuisine_id`

type scanner interface{ Scan(...any) error }

func scanRecipe(sc scanner) (*Recipe, error) {
	var r Recipe
	var meals, steps, created, updated string
	var deleted sql.NullString
	n := &r.Nutrition
	if err := sc.Scan(&r.ID, &r.Slug, &r.Name, &r.Description, &r.CuisineID, &r.Cuisine, &meals, &r.Difficulty,
		&r.PrepMinutes, &r.CookMinutes, &r.Servings, &r.VideoURL, &r.ImageURL, &steps, &n.Calories, &n.Protein,
		&n.Fat, &n.Carbs, &n.Fiber, &n.Sugar, &n.Sodium, &n.SaturatedFat, &r.Source, &r.SourceURL,
		&r.CanonicalURL, &r.DedupKey, &r.Status, &r.NeedsReview, &created, &updated, &deleted); err != nil {
		return nil, err
	}
	r.MealTimes = ParseMealTimes(meals)
	_ = json.Unmarshal([]byte(steps), &r.Steps)
	r.CreatedAt, r.UpdatedAt = db.ParseTime(created), db.ParseTime(updated)
	if deleted.Valid {
		t := db.ParseTime(deleted.String)
		r.DeletedAt = &t
	}
	return &r, nil
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getTx(ctx context.Context, q querier, id int64) (*Recipe, error) {
	r, err := scanRecipe(q.QueryRowContext(ctx, `SELECT `+recipeCols+` WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := loadChildren(ctx, q, []*Recipe{r}); err != nil {
		return nil, err
	}
	return r, nil
}

// Get returns a recipe by id, including soft-deleted ones.
func (s *Store) Get(ctx context.Context, id int64) (*Recipe, error) { return getTx(ctx, s.db, id) }

// GetBySlug returns a recipe by slug, including soft-deleted ones.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*Recipe, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM recipes WHERE slug = ?`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// FindDuplicate looks up an existing recipe by canonical URL, then by
// content key. Returns nil, nil when none exists.
func (s *Store) FindDuplicate(ctx context.Context, canonicalURL, contentKey string) (*Recipe, error) {
	var id int64
	var err error
	if cu := CanonicalizeURL(canonicalURL); cu != "" {
		err = s.db.QueryRowContext(ctx, `SELECT id FROM recipes WHERE canonical_url = ?`, cu).Scan(&id)
		if err == nil {
			return s.Get(ctx, id)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	err = s.db.QueryRowContext(ctx, `SELECT id FROM recipes WHERE dedup_key = ?
		ORDER BY (source = 'user') DESC, (status = 'approved') DESC, id LIMIT 1`, contentKey).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// List returns recipes matching f, with ingredients and translations loaded.
func (s *Store) List(ctx context.Context, f Filter) ([]*Recipe, error) {
	var where []string
	var args []any
	statuses := f.Statuses
	if len(statuses) == 0 {
		statuses = []string{StatusApproved}
	}
	where = append(where, "r.status IN ("+placeholders(len(statuses))+")")
	for _, st := range statuses {
		args = append(args, st)
	}
	switch f.Deleted {
	case DeletedExclude:
		where = append(where, "r.deleted_at IS NULL")
	case DeletedOnly:
		where = append(where, "r.deleted_at IS NOT NULL")
	}
	if f.Cuisine != "" {
		where = append(where, "c.slug = ?")
		args = append(args, f.Cuisine)
	}
	if f.MealTime != "" {
		where = append(where, "(',' || r.meal_times || ',') LIKE ?")
		args = append(args, "%,"+f.MealTime+",%")
	}
	if f.Difficulty != "" {
		where = append(where, "r.difficulty = ?")
		args = append(args, f.Difficulty)
	}
	if f.Source != "" {
		if strings.HasSuffix(f.Source, ":") {
			where = append(where, "r.source LIKE ?")
			args = append(args, f.Source+"%")
		} else {
			where = append(where, "r.source = ?")
			args = append(args, f.Source)
		}
	}
	if f.NeedsReview {
		where = append(where, "r.needs_review = 1")
	}
	if len(f.ExcludeIngredientIDs) > 0 {
		where = append(where, "NOT EXISTS (SELECT 1 FROM recipe_ingredients ri WHERE ri.recipe_id = r.id AND ri.ingredient_id IN ("+
			placeholders(len(f.ExcludeIngredientIDs))+"))")
		for _, id := range f.ExcludeIngredientIDs {
			args = append(args, id)
		}
	}
	if len(f.IDs) > 0 {
		where = append(where, "r.id IN ("+placeholders(len(f.IDs))+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	q := `SELECT ` + recipeCols + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY r.name, r.id`
	// Text search runs in Go (Unicode-aware), so paging happens afterwards.
	if f.Query == "" && f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, max(f.Offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []*Recipe
	for rows.Next() {
		r, err := scanRecipe(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := loadChildren(ctx, s.db, out); err != nil {
		return nil, err
	}
	if f.Query != "" {
		out = filterQuery(out, f.Query)
		out = paginate(out, f.Offset, f.Limit)
	}
	return out, nil
}

func filterQuery(list []*Recipe, query string) []*Recipe {
	q := ingredients.Normalize(query)
	var out []*Recipe
	for _, r := range list {
		hay := []string{r.Name, r.Cuisine}
		for _, t := range r.Translations {
			hay = append(hay, t.Name)
		}
		for _, h := range hay {
			if strings.Contains(ingredients.Normalize(h), q) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func paginate(list []*Recipe, offset, limit int) []*Recipe {
	if offset > len(list) {
		return nil
	}
	list = list[max(offset, 0):]
	if limit > 0 && limit < len(list) {
		list = list[:limit]
	}
	return list
}

func loadChildren(ctx context.Context, q querier, list []*Recipe) error {
	if len(list) == 0 {
		return nil
	}
	byID := make(map[int64]*Recipe, len(list))
	ids := make([]any, 0, len(list))
	for _, r := range list {
		byID[r.ID] = r
		ids = append(ids, r.ID)
		r.Ingredients = nil
		r.Translations = map[string]Translation{}
	}
	in := placeholders(len(ids))
	rows, err := q.QueryContext(ctx, `SELECT recipe_id, position, COALESCE(ingredient_id, 0), raw, quantity, unit, name,
		note, essential, needs_review FROM recipe_ingredients WHERE recipe_id IN (`+in+`) ORDER BY recipe_id, position`, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var rid int64
		var in Ingredient
		var qty sql.NullFloat64
		if err := rows.Scan(&rid, &in.Position, &in.IngredientID, &in.Raw, &qty, &in.Unit, &in.Name, &in.Note,
			&in.Essential, &in.NeedsReview); err != nil {
			rows.Close()
			return err
		}
		if qty.Valid {
			v := qty.Float64
			in.Quantity = &v
		}
		byID[rid].Ingredients = append(byID[rid].Ingredients, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	trows, err := q.QueryContext(ctx, `SELECT recipe_id, locale, name, description, steps_json, ingredients_json
		FROM recipe_i18n WHERE recipe_id IN (`+in+`)`, ids...)
	if err != nil {
		return err
	}
	defer trows.Close()
	for trows.Next() {
		var rid int64
		var loc, steps, ings string
		var t Translation
		if err := trows.Scan(&rid, &loc, &t.Name, &t.Description, &steps, &ings); err != nil {
			return err
		}
		_ = json.Unmarshal([]byte(steps), &t.Steps)
		_ = json.Unmarshal([]byte(ings), &t.Ingredients)
		byID[rid].Translations[loc] = t
	}
	return trows.Err()
}

// Revisions lists a recipe's revisions, newest first.
func (s *Store) Revisions(ctx context.Context, recipeID int64) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, recipe_id, rev, source, note, applied, snapshot_json, created_at
		FROM recipe_revisions WHERE recipe_id = ? ORDER BY rev DESC`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		var rv Revision
		var created string
		if err := rows.Scan(&rv.ID, &rv.RecipeID, &rv.Rev, &rv.Source, &rv.Note, &rv.Applied, &rv.Snapshot, &created); err != nil {
			return nil, err
		}
		rv.CreatedAt = db.ParseTime(created)
		out = append(out, rv)
	}
	return out, rows.Err()
}

// Counts returns recipe totals by status for non-deleted rows.
func (s *Store) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM recipes WHERE deleted_at IS NULL GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

func uniqueSlug(ctx context.Context, tx *sql.Tx, base string, selfID int64) (string, error) {
	base = Slugify(base)
	slug := base
	for i := 2; ; i++ {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM recipes WHERE slug = ?`, slug).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && id == selfID) {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
