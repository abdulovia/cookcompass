package shops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/abdulovia/cookcompass/internal/db"
)

// ErrNotFound is returned for missing shops.
var ErrNotFound = errors.New("shop not found")

// Store persists shops.
type Store struct{ db *sql.DB }

// NewStore returns a Store.
func NewStore(d *sql.DB) *Store { return &Store{db: d} }

const shopCols = `id, country_code, slug, name, website, search_url_template, active`

func scanShop(sc interface{ Scan(...any) error }) (*Shop, error) {
	var s Shop
	var active int
	if err := sc.Scan(&s.ID, &s.CountryCode, &s.Slug, &s.Name, &s.Website, &s.SearchURLTemplate, &active); err != nil {
		return nil, err
	}
	s.Active = active == 1
	return &s, nil
}

// Upsert creates or updates a shop by (country, slug). It is the seeding
// path: existing rows keep their id and user-made activeness is preserved
// only when the seed row is inactive — deactivating in the seed deactivates
// everywhere, which is what "this shop closed" means.
func (s *Store) Upsert(ctx context.Context, sh *Shop) error {
	if err := validate(sh); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO shops (country_code, slug, name, website, search_url_template, active)
		VALUES (?,?,?,?,?,?) ON CONFLICT (country_code, slug) DO UPDATE SET name=excluded.name,
		website=excluded.website, search_url_template=excluded.search_url_template, active=excluded.active,
		updated_at=strftime('%Y-%m-%dT%H:%M:%SZ', 'now')`,
		sh.CountryCode, sh.Slug, sh.Name, sh.Website, sh.SearchURLTemplate, boolInt(sh.Active))
	return err
}

// Create inserts a new shop.
func (s *Store) Create(ctx context.Context, sh *Shop) error {
	if err := validate(sh); err != nil {
		return err
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO shops (country_code, slug, name, website, search_url_template, active)
		VALUES (?,?,?,?,?,?) RETURNING id`,
		sh.CountryCode, sh.Slug, sh.Name, sh.Website, sh.SearchURLTemplate, boolInt(sh.Active)).Scan(&sh.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ValidationError{"slug": "validation.duplicate"}
	}
	return err
}

// Update saves changes to an existing shop.
func (s *Store) Update(ctx context.Context, sh *Shop) error {
	if sh.ID == 0 {
		return ErrNotFound
	}
	if err := validate(sh); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE shops SET country_code=?, slug=?, name=?, website=?,
		search_url_template=?, active=?, updated_at=? WHERE id=?`,
		sh.CountryCode, sh.Slug, sh.Name, sh.Website, sh.SearchURLTemplate, boolInt(sh.Active), db.Now(), sh.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ValidationError{"slug": "validation.duplicate"}
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a shop. Its products fall back to the country's generic
// catalog (products.shop_id is set to NULL).
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shops WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns one shop by id.
func (s *Store) Get(ctx context.Context, id int64) (*Shop, error) {
	sh, err := scanShop(s.db.QueryRowContext(ctx, `SELECT `+shopCols+` FROM shops WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sh, err
}

// List returns shops ordered by name. With country set, only that country's
// shops are returned; with activeOnly, inactive ones are skipped.
func (s *Store) List(ctx context.Context, country string, activeOnly bool) ([]*Shop, error) {
	q := `SELECT ` + shopCols + ` FROM shops`
	var args []any
	var where []string
	if country != "" {
		where = append(where, "country_code = ?")
		args = append(args, strings.ToUpper(country))
	}
	if activeOnly {
		where = append(where, "active = 1")
	}
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY country_code, name`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Shop
	for rows.Next() {
		sh, err := scanShop(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// IDBySlug resolves a (country, slug) pair to a shop id. It returns 0 when
// the shop is unknown, so callers can fall back to the generic catalog.
func (s *Store) IDBySlug(ctx context.Context, country, slug string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM shops WHERE country_code = ? AND slug = ?`,
		strings.ToUpper(country), strings.TrimSpace(slug)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("shop %s/%s: %w", country, slug, ErrNotFound)
	}
	return id, err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
