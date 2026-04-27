// Package users stores the local user, their settings, food preferences,
// pantry, sessions and cooking history.
package users

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/abdulovia/cookcompass/internal/db"
)

// DefaultUsername is the account used in single-user mode.
const DefaultUsername = "cook"

// Preference kinds.
const (
	Dislike = "dislike"
	Allergy = "allergy"
)

// Errors.
var (
	ErrNotFound     = errors.New("user not found")
	ErrBadLogin     = errors.New("wrong username or password")
	ErrUsernameUsed = errors.New("username already taken")
)

// Settings are per-user preferences.
type Settings struct {
	CountryCode     string
	CityID          int64
	Units           string // metric | imperial
	Locale          string // "" = follow browser
	ShowUnavailable bool
	ShopID          int64 // preferred grocery shop; 0 = any
	StrictShop      bool  // availability only from ShopID + generic products
	HeightCm        float64
	WeightKg        float64
	Age             int
	Sex             string
	Activity        string
	Goal            string
}

// Profile is a user with everything needed to personalise pages.
type Profile struct {
	ID       int64
	Username string
	Settings
	Prefs  map[int64]string // ingredient id → dislike | allergy
	Pantry map[int64]bool
}

// HasGoal reports whether the nutrition goal form has been completed.
func (p *Profile) HasGoal() bool {
	return p.HeightCm > 0 && p.WeightKg > 0 && p.Age > 0 && p.Sex != "" && p.Activity != "" && p.Goal != ""
}

// Excluded returns ingredient ids to hide: allergies always, dislikes
// when includeDislikes is set.
func (p *Profile) Excluded(includeDislikes bool) []int64 {
	var out []int64
	for id, kind := range p.Prefs {
		if kind == Allergy || includeDislikes {
			out = append(out, id)
		}
	}
	return out
}

// Store persists users.
type Store struct{ db *sql.DB }

// NewStore returns a Store.
func NewStore(d *sql.DB) *Store { return &Store{db: d} }

// EnsureDefault creates the single-user account if missing and returns its id.
func (s *Store) EnsureDefault(ctx context.Context, country string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ?`, DefaultUsername).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return s.create(ctx, DefaultUsername, "", country)
}

// Create registers a new user with a bcrypt-hashed password.
func (s *Store) Create(ctx context.Context, username, password, country string) (int64, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if len(username) < 3 || len(username) > 40 || strings.ContainsAny(username, " \t\n") {
		return 0, fmt.Errorf("username must be 3–40 characters without spaces")
	}
	if len(password) < 8 {
		return 0, fmt.Errorf("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	return s.create(ctx, username, string(hash), country)
}

func (s *Store) create(ctx context.Context, username, hash, country string) (int64, error) {
	var id int64
	err := db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO users (username, password_hash) VALUES (?, ?) RETURNING id`,
			username, hash).Scan(&id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrUsernameUsed
			}
			return err
		}
		var cc any
		if country != "" {
			cc = strings.ToUpper(country)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, country_code) VALUES (?, ?)`, id, cc)
		return err
	})
	return id, err
}

// Authenticate checks a username/password pair.
func (s *Store) Authenticate(ctx context.Context, username, password string) (int64, error) {
	var id int64
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE username = ?`,
		strings.ToLower(strings.TrimSpace(username))).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && hash == "") {
		// Equalize timing a little for unknown users.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5BWX4Z6KJ7O3rK7oVhC6gKXm8QH6S"), []byte(password))
		return 0, ErrBadLogin
	}
	if err != nil {
		return 0, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return 0, ErrBadLogin
	}
	return id, nil
}

// Profile loads a user with settings, preferences and pantry.
func (s *Store) Profile(ctx context.Context, id int64) (*Profile, error) {
	p := &Profile{ID: id, Prefs: map[int64]string{}, Pantry: map[int64]bool{}}
	var country sql.NullString
	var city, shop sql.NullInt64
	var strict int
	err := s.db.QueryRowContext(ctx, `SELECT u.username, s.country_code, s.city_id, s.units, s.locale, s.show_unavailable,
		s.shop_id, s.strict_shop, s.height_cm, s.weight_kg, s.age, s.sex, s.activity, s.goal
		FROM users u JOIN user_settings s ON s.user_id = u.id WHERE u.id = ?`, id).Scan(&p.Username, &country, &city,
		&p.Units, &p.Locale, &p.ShowUnavailable, &shop, &strict, &p.HeightCm, &p.WeightKg, &p.Age, &p.Sex, &p.Activity, &p.Goal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CountryCode, p.CityID, p.ShopID, p.StrictShop = country.String, city.Int64, shop.Int64, strict == 1
	rows, err := s.db.QueryContext(ctx, `SELECT ingredient_id, kind FROM user_dislikes WHERE user_id = ?`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ing int64
		var kind string
		if err := rows.Scan(&ing, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		p.Prefs[ing] = kind
	}
	rows.Close()
	prow, err := s.db.QueryContext(ctx, `SELECT ingredient_id FROM user_pantry WHERE user_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer prow.Close()
	for prow.Next() {
		var ing int64
		if err := prow.Scan(&ing); err != nil {
			return nil, err
		}
		p.Pantry[ing] = true
	}
	return p, prow.Err()
}

// SaveSettings replaces a user's settings.
func (s *Store) SaveSettings(ctx context.Context, id int64, st Settings) error {
	if st.Units != "imperial" {
		st.Units = "metric"
	}
	var country, city, shop any
	if st.CountryCode != "" {
		country = strings.ToUpper(st.CountryCode)
	}
	if st.CityID != 0 {
		city = st.CityID
	}
	if st.ShopID != 0 {
		shop = st.ShopID
	}
	res, err := s.db.ExecContext(ctx, `UPDATE user_settings SET country_code=?, city_id=?, units=?, locale=?, show_unavailable=?,
		shop_id=?, strict_shop=?, height_cm=?, weight_kg=?, age=?, sex=?, activity=?, goal=?, updated_at=? WHERE user_id=?`,
		country, city, st.Units, st.Locale, st.ShowUnavailable, shop, st.StrictShop, st.HeightCm, st.WeightKg, st.Age,
		st.Sex, st.Activity, st.Goal, db.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPreferences replaces the user's allergies and dislikes.
func (s *Store) SetPreferences(ctx context.Context, id int64, prefs map[int64]string) error {
	return db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_dislikes WHERE user_id = ?`, id); err != nil {
			return err
		}
		for ing, kind := range prefs {
			if kind != Allergy {
				kind = Dislike
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_dislikes (user_id, ingredient_id, kind) VALUES (?,?,?)`, id, ing, kind); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetPantry replaces the ingredients the user has at home.
func (s *Store) SetPantry(ctx context.Context, id int64, ingredientIDs []int64) error {
	return db.Tx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_pantry WHERE user_id = ?`, id); err != nil {
			return err
		}
		for _, ing := range ingredientIDs {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_pantry (user_id, ingredient_id) VALUES (?,?)`, id, ing); err != nil {
				return err
			}
		}
		return nil
	})
}

// SessionTTL is how long a login lasts.
const SessionTTL = 30 * 24 * time.Hour

// NewSession creates a random session token for userID.
func (s *Store) NewSession(ctx context.Context, userID int64) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, expires_at) VALUES (?,?,?)`,
		token, userID, time.Now().Add(SessionTTL).UTC().Format(db.TimeFormat))
	return token, err
}

// SessionUser resolves a non-expired session token.
func (s *Store) SessionUser(ctx context.Context, token string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE token = ? AND expires_at > ?`,
		token, db.Now()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// LogCooked records that the user cooked a recipe.
func (s *Store) LogCooked(ctx context.Context, userID, recipeID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO cooked_log (user_id, recipe_id, cooked_at) VALUES (?,?,?)`, userID, recipeID, db.Now())
	return err
}

// CookedCounts returns how many times the user cooked each recipe.
func (s *Store) CookedCounts(ctx context.Context, userID int64) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT recipe_id, COUNT(*) FROM cooked_log WHERE user_id = ? GROUP BY recipe_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// CookEvent is one entry of the cooking history.
type CookEvent struct {
	RecipeID int64
	At       time.Time
}

// RecentCooked returns the latest cooking events, newest first.
func (s *Store) RecentCooked(ctx context.Context, userID int64, limit int) ([]CookEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT recipe_id, cooked_at FROM cooked_log WHERE user_id = ? ORDER BY cooked_at DESC, id DESC LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CookEvent
	for rows.Next() {
		var e CookEvent
		var at string
		if err := rows.Scan(&e.RecipeID, &at); err != nil {
			return nil, err
		}
		e.At = db.ParseTime(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetLearned marks or unmarks a recipe as learned.
func (s *Store) SetLearned(ctx context.Context, userID, recipeID int64, learned bool) error {
	var err error
	if learned {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO learned (user_id, recipe_id, learned_at) VALUES (?,?,?)`, userID, recipeID, db.Now())
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM learned WHERE user_id = ? AND recipe_id = ?`, userID, recipeID)
	}
	return err
}

// Learned returns the user's learned recipes and when they were learned.
func (s *Store) Learned(ctx context.Context, userID int64) (map[int64]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT recipe_id, learned_at FROM learned WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]time.Time{}
	for rows.Next() {
		var id int64
		var at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = db.ParseTime(at)
	}
	return out, rows.Err()
}
