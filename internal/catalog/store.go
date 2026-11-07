// Package catalog manages countries, cities and per-country grocery
// products, and answers "which ingredients can I buy here?".
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/abdulovia/cookcompass/internal/db"
	"github.com/abdulovia/cookcompass/internal/ingredients"
)

// Errors.
var (
	ErrNotFound = errors.New("not found")
)

// Product statuses mirror recipe review states.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
)

// Country is a supported market.
type Country struct {
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Currency      string  `json:"currency"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
	DefaultLocale string  `json:"default_locale"`
}

// City is a known city used for location and reverse geocoding.
type City struct {
	ID          int64   `json:"id"`
	CountryCode string  `json:"country_code"`
	Name        string  `json:"name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// Product is a grocery item sold in a country.
type Product struct {
	ID             int64
	CountryCode    string
	SKU            string
	Name           string
	Category       string
	IngredientID   int64
	IngredientSlug string
	Price          float64
	Currency       string
	Unit           string
	Source         string
	SourceURL      string
	DedupKey       string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// Deleted reports whether the product is soft-deleted.
func (p *Product) Deleted() bool { return p.DeletedAt != nil }

// ValidationError maps field → i18n message id.
type ValidationError map[string]string

func (v ValidationError) Error() string {
	var parts []string
	for k, m := range v {
		parts = append(parts, k+": "+m)
	}
	return "invalid product: " + strings.Join(parts, "; ")
}

// ProductKey is the dedup key within a country: the SKU when known,
// otherwise the normalized product name.
func ProductKey(sku, name string) string {
	if s := strings.TrimSpace(sku); s != "" {
		return "sku:" + strings.ToLower(s)
	}
	return "name:" + ingredients.Key(name)
}

// Store persists catalog data.
type Store struct {
	db   *sql.DB
	ings *ingredients.Store
}

// NewStore returns a Store.
func NewStore(d *sql.DB, ings *ingredients.Store) *Store { return &Store{db: d, ings: ings} }

// UpsertCountry creates or updates a country.
func (s *Store) UpsertCountry(ctx context.Context, c Country) error {
	c.Code = strings.ToUpper(strings.TrimSpace(c.Code))
	if len(c.Code) != 2 || c.Name == "" {
		return ValidationError{"code": "validation.invalid_choice"}
	}
	if c.DefaultLocale == "" {
		c.DefaultLocale = "en"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO countries (code, name, currency, lat, lng, default_locale) VALUES (?,?,?,?,?,?)
		ON CONFLICT (code) DO UPDATE SET name=excluded.name, currency=excluded.currency, lat=excluded.lat, lng=excluded.lng,
		default_locale=excluded.default_locale`, c.Code, c.Name, c.Currency, c.Lat, c.Lng, c.DefaultLocale)
	return err
}

// Countries lists countries by name.
func (s *Store) Countries(ctx context.Context) ([]Country, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT code, name, currency, lat, lng, default_locale FROM countries ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Country
	for rows.Next() {
		var c Country
		if err := rows.Scan(&c.Code, &c.Name, &c.Currency, &c.Lat, &c.Lng, &c.DefaultLocale); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Country returns one country.
func (s *Store) Country(ctx context.Context, code string) (Country, error) {
	var c Country
	err := s.db.QueryRowContext(ctx, `SELECT code, name, currency, lat, lng, default_locale FROM countries WHERE code = ?`,
		strings.ToUpper(code)).Scan(&c.Code, &c.Name, &c.Currency, &c.Lat, &c.Lng, &c.DefaultLocale)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("country %s: %w", code, ErrNotFound)
	}
	return c, err
}

// UpsertCity creates or updates a city by (country, name).
func (s *Store) UpsertCity(ctx context.Context, c City) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO cities (country_code, name, lat, lng) VALUES (?,?,?,?)
		ON CONFLICT (country_code, name) DO UPDATE SET lat=excluded.lat, lng=excluded.lng RETURNING id`,
		strings.ToUpper(c.CountryCode), c.Name, c.Lat, c.Lng).Scan(&id)
	return id, err
}

// Cities lists cities, optionally for one country.
func (s *Store) Cities(ctx context.Context, country string) ([]City, error) {
	q := `SELECT id, country_code, name, lat, lng FROM cities`
	var args []any
	if country != "" {
		q += ` WHERE country_code = ?`
		args = append(args, strings.ToUpper(country))
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY country_code, name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []City
	for rows.Next() {
		var c City
		if err := rows.Scan(&c.ID, &c.CountryCode, &c.Name, &c.Lat, &c.Lng); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// City returns one city.
func (s *Store) City(ctx context.Context, id int64) (City, error) {
	var c City
	err := s.db.QueryRowContext(ctx, `SELECT id, country_code, name, lat, lng FROM cities WHERE id = ?`, id).
		Scan(&c.ID, &c.CountryCode, &c.Name, &c.Lat, &c.Lng)
	if errors.Is(err, sql.ErrNoRows) {
		return c, fmt.Errorf("city %d: %w", id, ErrNotFound)
	}
	return c, err
}

// NearestCity reverse-geocodes coordinates against the bundled city list,
// returning the closest city and its distance in km.
func (s *Store) NearestCity(ctx context.Context, lat, lng float64) (City, float64, error) {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return City{}, 0, ValidationError{"coords": "validation.out_of_range"}
	}
	cities, err := s.Cities(ctx, "")
	if err != nil {
		return City{}, 0, err
	}
	if len(cities) == 0 {
		return City{}, 0, ErrNotFound
	}
	best, bestKm := cities[0], math.Inf(1)
	for _, c := range cities {
		if d := Haversine(lat, lng, c.Lat, c.Lng); d < bestKm {
			best, bestKm = c, d
		}
	}
	return best, bestKm, nil
}

// Haversine returns the great-circle distance in km.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// prepare validates p and resolves its ingredient.
func (s *Store) prepare(ctx context.Context, p *Product) error {
	errs := ValidationError{}
	p.Name = strings.TrimSpace(p.Name)
	p.CountryCode = strings.ToUpper(strings.TrimSpace(p.CountryCode))
	p.SKU = strings.TrimSpace(p.SKU)
	if p.Name == "" {
		errs["name"] = "validation.required"
	}
	if p.Price < 0 || p.Price > 1e7 {
		errs["price"] = "validation.out_of_range"
	}
	c, err := s.Country(ctx, p.CountryCode)
	if err != nil {
		errs["country"] = "validation.invalid_choice"
	} else if p.Currency == "" {
		p.Currency = c.Currency
	}
	if p.IngredientSlug != "" {
		id, err := s.ings.IDBySlug(ctx, p.IngredientSlug)
		if err != nil {
			errs["ingredient"] = "validation.invalid_choice"
		}
		p.IngredientID = id
	} else if p.IngredientID == 0 && p.Name != "" {
		m, err := s.ings.Matcher(ctx)
		if err != nil {
			return err
		}
		if id, ok := m.Match(stripPack(p.Name)); ok {
			p.IngredientID = id
		}
	}
	if len(errs) > 0 {
		return errs
	}
	if p.Category == "" {
		p.Category = "other"
	}
	if p.Status == "" {
		p.Status = StatusApproved
	}
	if p.Source == "" {
		p.Source = "user"
	}
	p.DedupKey = ProductKey(p.SKU, p.Name)
	return nil
}

// stripPack removes a trailing ", 500 g" pack size from a product name.
func stripPack(name string) string {
	if i := strings.LastIndex(name, ","); i > 0 {
		return name[:i]
	}
	return name
}

// CreateProduct inserts a product.
func (s *Store) CreateProduct(ctx context.Context, p *Product) error {
	if err := s.prepare(ctx, p); err != nil {
		return err
	}
	now := db.Now()
	err := s.db.QueryRowContext(ctx, `INSERT INTO products (country_code, sku, name, category, ingredient_id, price, currency,
		unit, source, source_url, dedup_key, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
		p.CountryCode, p.SKU, p.Name, p.Category, nullID(p.IngredientID), p.Price, p.Currency, p.Unit, p.Source,
		p.SourceURL, p.DedupKey, p.Status, now, now).Scan(&p.ID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ValidationError{"sku": "validation.duplicate"}
	}
	return err
}

// UpdateProduct saves changes to an existing product.
func (s *Store) UpdateProduct(ctx context.Context, p *Product) error {
	if p.ID == 0 {
		return ErrNotFound
	}
	if err := s.prepare(ctx, p); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE products SET country_code=?, sku=?, name=?, category=?, ingredient_id=?, price=?,
		currency=?, unit=?, source=?, source_url=?, dedup_key=?, status=?, updated_at=? WHERE id=?`,
		p.CountryCode, p.SKU, p.Name, p.Category, nullID(p.IngredientID), p.Price, p.Currency, p.Unit, p.Source,
		p.SourceURL, p.DedupKey, p.Status, db.Now(), p.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ValidationError{"sku": "validation.duplicate"}
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertResult describes what UpsertProduct did.
type UpsertResult string

// Upsert outcomes.
const (
	Created UpsertResult = "created"
	Updated UpsertResult = "updated"
	Skipped UpsertResult = "skipped"
)

// UpsertProduct inserts p or updates the existing product with the same
// dedup key. Products edited by a user (source "user") are never
// overwritten; neither are existing rows when onlyNew is set (seeding).
func (s *Store) UpsertProduct(ctx context.Context, p *Product, onlyNew bool) (UpsertResult, error) {
	if err := s.prepare(ctx, p); err != nil {
		return Skipped, err
	}
	existing, err := s.FindProduct(ctx, p.CountryCode, p.DedupKey)
	if errors.Is(err, ErrNotFound) {
		return Created, s.CreateProduct(ctx, p)
	}
	if err != nil {
		return Skipped, err
	}
	if onlyNew || existing.Source == "user" {
		return Skipped, nil
	}
	p.ID = existing.ID
	if existing.Status != StatusPending {
		p.Status = existing.Status // re-scans keep the review decision
	}
	return Updated, s.UpdateProduct(ctx, p)
}

const productCols = `p.id, p.country_code, p.sku, p.name, p.category, COALESCE(p.ingredient_id, 0), COALESCE(i.slug, ''),
	p.price, p.currency, p.unit, p.source, p.source_url, p.dedup_key, p.status, p.created_at, p.updated_at, p.deleted_at
	FROM products p LEFT JOIN ingredients i ON i.id = p.ingredient_id`

func scanProduct(sc interface{ Scan(...any) error }) (*Product, error) {
	var p Product
	var created, updated string
	var deleted sql.NullString
	if err := sc.Scan(&p.ID, &p.CountryCode, &p.SKU, &p.Name, &p.Category, &p.IngredientID, &p.IngredientSlug, &p.Price,
		&p.Currency, &p.Unit, &p.Source, &p.SourceURL, &p.DedupKey, &p.Status, &created, &updated, &deleted); err != nil {
		return nil, err
	}
	p.CreatedAt, p.UpdatedAt = db.ParseTime(created), db.ParseTime(updated)
	if deleted.Valid {
		t := db.ParseTime(deleted.String)
		p.DeletedAt = &t
	}
	return &p, nil
}

// GetProduct returns a product by id (including deleted).
func (s *Store) GetProduct(ctx context.Context, id int64) (*Product, error) {
	p, err := scanProduct(s.db.QueryRowContext(ctx, `SELECT `+productCols+` WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// FindProduct looks up a product by country and dedup key.
func (s *Store) FindProduct(ctx context.Context, country, key string) (*Product, error) {
	p, err := scanProduct(s.db.QueryRowContext(ctx, `SELECT `+productCols+` WHERE p.country_code = ? AND p.dedup_key = ?`,
		strings.ToUpper(country), key))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ProductFilter narrows ListProducts. Zero value = approved, not deleted.
type ProductFilter struct {
	Country      string
	Statuses     []string
	Deleted      int // 0 exclude, 1 include, 2 only
	Category     string
	Query        string
	Unmapped     bool
	IngredientID int64
	Limit        int
	Offset       int
}

// ListProducts returns matching products ordered by country, category, name.
func (s *Store) ListProducts(ctx context.Context, f ProductFilter) ([]*Product, error) {
	where := []string{}
	var args []any
	statuses := f.Statuses
	if len(statuses) == 0 {
		statuses = []string{StatusApproved}
	}
	where = append(where, "p.status IN ("+strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")+")")
	for _, st := range statuses {
		args = append(args, st)
	}
	switch f.Deleted {
	case 0:
		where = append(where, "p.deleted_at IS NULL")
	case 2:
		where = append(where, "p.deleted_at IS NOT NULL")
	}
	if f.Country != "" {
		where = append(where, "p.country_code = ?")
		args = append(args, strings.ToUpper(f.Country))
	}
	if f.Category != "" {
		where = append(where, "p.category = ?")
		args = append(args, f.Category)
	}
	if f.Unmapped {
		where = append(where, "p.ingredient_id IS NULL")
	}
	if f.IngredientID != 0 {
		where = append(where, "p.ingredient_id = ?")
		args = append(args, f.IngredientID)
	}
	q := `SELECT ` + productCols + ` WHERE ` + strings.Join(where, " AND ") + ` ORDER BY p.country_code, p.category, p.name`
	if f.Query == "" && f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", f.Limit, max(f.Offset, 0))
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Product
	needle := ingredients.Normalize(f.Query)
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		if needle != "" && !strings.Contains(ingredients.Normalize(p.Name+" "+p.SKU+" "+p.IngredientSlug), needle) {
			continue
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if f.Query != "" && f.Limit > 0 {
		start := min(max(f.Offset, 0), len(out))
		out = out[start:min(start+f.Limit, len(out))]
	}
	return out, nil
}

// SoftDeleteProduct hides a product.
func (s *Store) SoftDeleteProduct(ctx context.Context, id int64) error {
	return s.exec1(ctx, `UPDATE products SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, db.Now(), db.Now(), id)
}

// RestoreProduct undoes SoftDeleteProduct.
func (s *Store) RestoreProduct(ctx context.Context, id int64) error {
	return s.exec1(ctx, `UPDATE products SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL`, db.Now(), id)
}

// SetProductStatus approves or rejects a product.
func (s *Store) SetProductStatus(ctx context.Context, id int64, status string) error {
	if status != StatusApproved && status != StatusRejected && status != StatusPending {
		return ValidationError{"status": "validation.invalid_choice"}
	}
	return s.exec1(ctx, `UPDATE products SET status = ?, updated_at = ? WHERE id = ?`, status, db.Now(), id)
}

// ApproveAllPending approves every pending product.
func (s *Store) ApproveAllPending(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE products SET status = 'approved', updated_at = ? WHERE status = 'pending'`, db.Now())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PurgePending deletes products still awaiting review.
func (s *Store) PurgePending(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM products WHERE status = 'pending'`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ProductCounts returns approved, non-deleted product counts per country.
func (s *Store) ProductCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT country_code, COUNT(*) FROM products
		WHERE status = 'approved' AND deleted_at IS NULL GROUP BY country_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

func (s *Store) exec1(ctx context.Context, q string, args ...any) error {
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// IngredientSet returns the ids of ingredients with at least one approved,
// non-deleted product in country.
func (s *Store) IngredientSet(ctx context.Context, country string) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT ingredient_id FROM products
		WHERE country_code = ? AND ingredient_id IS NOT NULL AND status = 'approved' AND deleted_at IS NULL`,
		strings.ToUpper(country))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
