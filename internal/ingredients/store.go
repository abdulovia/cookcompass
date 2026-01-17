package ingredients

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/abdulovia/cookcompass/internal/db"
)

// Ingredient is a canonical ingredient.
type Ingredient struct {
	ID       int64             `json:"id"`
	Slug     string            `json:"slug"`
	Name     string            `json:"name"`
	Category string            `json:"category"`
	Staple   bool              `json:"staple"`
	Names    map[string]string `json:"names,omitempty"` // locale → name
	Aliases  []string          `json:"aliases,omitempty"`
}

// LocalName returns the name in locale, falling back to the canonical name.
func (i Ingredient) LocalName(locale string) string {
	if n := i.Names[locale]; n != "" {
		return n
	}
	return i.Name
}

// Substitute links an ingredient to a replacement.
type Substitute struct {
	IngredientID int64  `json:"ingredient_id"`
	SubstituteID int64  `json:"substitute_id"`
	Ratio        string `json:"ratio"`
}

// ErrNotFound is returned when an ingredient does not exist.
var ErrNotFound = errors.New("ingredient not found")

// Store persists ingredients and caches a matcher.
type Store struct {
	db *sql.DB

	mu      sync.RWMutex
	matcher *Matcher
}

// NewStore returns a Store.
func NewStore(d *sql.DB) *Store { return &Store{db: d} }

// Upsert inserts or updates an ingredient by slug together with its
// translations and aliases, returning its id.
func (s *Store) Upsert(ctx context.Context, in Ingredient) (int64, error) {
	if in.Slug == "" || in.Name == "" {
		return 0, errors.New("ingredient slug and name are required")
	}
	if in.Category == "" {
		in.Category = "other"
	}
	var id int64
	err := db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO ingredients (slug, name, category, staple) VALUES (?, ?, ?, ?)
			ON CONFLICT (slug) DO UPDATE SET name = excluded.name, category = excluded.category, staple = excluded.staple
			RETURNING id`, in.Slug, in.Name, in.Category, in.Staple).Scan(&id)
		if err != nil {
			return err
		}
		for loc, name := range in.Names {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ingredient_i18n (ingredient_id, locale, name) VALUES (?, ?, ?)
				ON CONFLICT (ingredient_id, locale) DO UPDATE SET name = excluded.name`, id, loc, name); err != nil {
				return err
			}
		}
		aliases := append([]string{in.Name, strings.ReplaceAll(in.Slug, "-", " ")}, in.Aliases...)
		for _, n := range in.Names {
			aliases = append(aliases, n)
		}
		for _, a := range aliases {
			k := Key(a)
			if k == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ingredient_aliases (alias, ingredient_id) VALUES (?, ?)
				ON CONFLICT (alias) DO UPDATE SET ingredient_id = excluded.ingredient_id`, k, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("upsert ingredient %s: %w", in.Slug, err)
	}
	s.invalidate()
	return id, nil
}

// SetSubstitute declares that sub can replace ing (both by slug).
func (s *Store) SetSubstitute(ctx context.Context, ing, sub, ratio string) error {
	if ratio == "" {
		ratio = "1:1"
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO ingredient_substitutes (ingredient_id, substitute_id, ratio)
		SELECT a.id, b.id, ? FROM ingredients a, ingredients b WHERE a.slug = ? AND b.slug = ?
		ON CONFLICT (ingredient_id, substitute_id) DO UPDATE SET ratio = excluded.ratio`, ratio, ing, sub)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("substitute %s → %s: %w", ing, sub, ErrNotFound)
	}
	return nil
}

// List returns all ingredients ordered by category and name, with translations.
func (s *Store) List(ctx context.Context) ([]Ingredient, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, category, staple FROM ingredients ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	var out []Ingredient
	idx := map[int64]int{}
	for rows.Next() {
		var i Ingredient
		if err := rows.Scan(&i.ID, &i.Slug, &i.Name, &i.Category, &i.Staple); err != nil {
			rows.Close()
			return nil, err
		}
		i.Names = map[string]string{}
		idx[i.ID] = len(out)
		out = append(out, i)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	trows, err := s.db.QueryContext(ctx, `SELECT ingredient_id, locale, name FROM ingredient_i18n`)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var id int64
		var loc, name string
		if err := trows.Scan(&id, &loc, &name); err != nil {
			return nil, err
		}
		if k, ok := idx[id]; ok {
			out[k].Names[loc] = name
		}
	}
	return out, trows.Err()
}

// ByID returns ingredients keyed by id.
func (s *Store) ByID(ctx context.Context) (map[int64]Ingredient, error) {
	list, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]Ingredient, len(list))
	for _, i := range list {
		m[i.ID] = i
	}
	return m, nil
}

// IDBySlug resolves a slug to an id.
func (s *Store) IDBySlug(ctx context.Context, slug string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM ingredients WHERE slug = ?`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%s: %w", slug, ErrNotFound)
	}
	return id, err
}

// Substitutes returns ingredient id → substitutes, ordered by substitute id.
func (s *Store) Substitutes(ctx context.Context) (map[int64][]Substitute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ingredient_id, substitute_id, ratio FROM ingredient_substitutes ORDER BY ingredient_id, substitute_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Substitute{}
	for rows.Next() {
		var sub Substitute
		if err := rows.Scan(&sub.IngredientID, &sub.SubstituteID, &sub.Ratio); err != nil {
			return nil, err
		}
		out[sub.IngredientID] = append(out[sub.IngredientID], sub)
	}
	return out, rows.Err()
}

// Matcher returns a cached matcher built from all aliases.
func (s *Store) Matcher(ctx context.Context) (*Matcher, error) {
	s.mu.RLock()
	m := s.matcher
	s.mu.RUnlock()
	if m != nil {
		return m, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT alias, ingredient_id FROM ingredient_aliases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	aliases := map[string]int64{}
	for rows.Next() {
		var a string
		var id int64
		if err := rows.Scan(&a, &id); err != nil {
			return nil, err
		}
		aliases[a] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	m = NewMatcher(aliases)
	s.mu.Lock()
	s.matcher = m
	s.mu.Unlock()
	return m, nil
}

func (s *Store) invalidate() {
	s.mu.Lock()
	s.matcher = nil
	s.mu.Unlock()
}

// Matcher maps free-text ingredient names to canonical ids.
type Matcher struct {
	aliases map[string]int64
	maxLen  int
}

// NewMatcher builds a matcher from normalized alias → id. Keys are passed
// through Key so callers may supply raw names.
func NewMatcher(aliases map[string]int64) *Matcher {
	m := &Matcher{aliases: make(map[string]int64, len(aliases))}
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic when two raw names share a key
	for _, k := range keys {
		nk := Key(k)
		m.aliases[nk] = aliases[k]
		if n := len(strings.Fields(nk)); n > m.maxLen {
			m.maxLen = n
		}
	}
	return m
}

// Match returns the ingredient id for name. It first tries the whole key,
// then the longest contiguous run of significant words, preferring runs
// further right (the head noun in English noun phrases).
func (m *Matcher) Match(name string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	if id, ok := m.aliases[Key(name)]; ok {
		return id, true
	}
	toks := Tokens(name)
	for size := min(len(toks), m.maxLen); size >= 1; size-- {
		for start := len(toks) - size; start >= 0; start-- {
			if id, ok := m.aliases[strings.Join(toks[start:start+size], " ")]; ok {
				return id, true
			}
		}
	}
	return 0, false
}
