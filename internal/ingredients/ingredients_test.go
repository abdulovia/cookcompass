package ingredients

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/abdulovia/cookcompass/internal/db"
)

func TestNormalizeAndKey(t *testing.T) {
	tests := []struct{ in, norm, key string }{
		{"  Fresh   TOMATOES, chopped ", "fresh tomatoes chopped", "fresh tomato chopped"},
		{"Crème fraîche", "creme fraiche", "creme fraiche"},
		{"Ёжик", "ежик", "ежик"},
		{"chickpeas (canned)", "chickpeas canned", "chickpea canned"},
		{"cherries", "cherries", "cherry"},
		{"leaves", "leaves", "leaf"},
		{"glass", "glass", "glass"},
		{"couscous", "couscous", "couscous"},
		{"dishes", "dishes", "dish"},
		{"O'Brien’s", "obriens", "obrien"},
	}
	for _, tc := range tests {
		if got := Normalize(tc.in); got != tc.norm {
			t.Errorf("Normalize(%q) = %q want %q", tc.in, got, tc.norm)
		}
		if got := Key(tc.in); got != tc.key {
			t.Errorf("Key(%q) = %q want %q", tc.in, got, tc.key)
		}
	}
}

func TestMatcher(t *testing.T) {
	m := NewMatcher(map[string]int64{
		"chicken":                1,
		"chicken stock":          2,
		"tomato":                 3,
		"canned tomatoes":        4,
		"olive oil":              5,
		"extra virgin olive oil": 5,
		"garlic":                 6,
		"молоко":                 7,
		"ground beef":            8,
		"cumin":                  9,
		"ground cumin":           9,
		"зелёная паста карри":    10,
		"карри":                  11,
	})
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"Chicken stock", 2, true},
		{"2 boneless skinless chicken breasts", 1, true},
		{"fresh tomatoes, chopped", 3, true},
		{"canned tomatoes", 4, true},
		{"extra-virgin olive oil", 5, true},
		{"3 cloves garlic, minced", 6, true},
		{"Молоко", 7, true},
		{"lean ground beef", 8, true},
		{"ground cumin", 9, true},
		{"unicorn tears", 0, false},
		{"Паста карри зелёная, 110 g", 10, true},
		{"карри", 11, true},
	}
	for _, tc := range tests {
		got, ok := m.Match(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Match(%q) = %d,%v want %d,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	var nilM *Matcher
	if _, ok := nilM.Match("x"); ok {
		t.Fatal("nil matcher must not match")
	}
}

func newStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := db.Migrate(d, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return NewStore(d)
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	butter, err := s.Upsert(ctx, Ingredient{Slug: "butter", Name: "butter", Category: "dairy", Names: map[string]string{"ru": "сливочное масло"}})
	if err != nil {
		t.Fatal(err)
	}
	oil, err := s.Upsert(ctx, Ingredient{Slug: "vegetable-oil", Name: "vegetable oil", Staple: true, Aliases: []string{"sunflower oil"}})
	if err != nil {
		t.Fatal(err)
	}
	// Upsert again updates in place.
	again, err := s.Upsert(ctx, Ingredient{Slug: "butter", Name: "butter", Category: "dairy-fat"})
	if err != nil || again != butter {
		t.Fatalf("re-upsert id %d err %v", again, err)
	}
	if _, err := s.Upsert(ctx, Ingredient{}); err == nil {
		t.Fatal("empty ingredient must fail")
	}
	if err := s.SetSubstitute(ctx, "butter", "vegetable-oil", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSubstitute(ctx, "butter", "nope", "1:1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	list, err := s.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list %v err %v", list, err)
	}
	byID, err := s.ByID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := byID[butter].LocalName("ru"); got != "сливочное масло" {
		t.Errorf("ru name %q", got)
	}
	if got := byID[butter].LocalName("de"); got != "butter" {
		t.Errorf("fallback name %q", got)
	}
	if byID[butter].Category != "dairy-fat" || !byID[oil].Staple {
		t.Errorf("fields not stored: %+v", byID)
	}

	subs, err := s.Substitutes(ctx)
	if err != nil || len(subs[butter]) != 1 || subs[butter][0].SubstituteID != oil || subs[butter][0].Ratio != "1:1" {
		t.Fatalf("subs %v err %v", subs, err)
	}

	id, err := s.IDBySlug(ctx, "butter")
	if err != nil || id != butter {
		t.Fatalf("IDBySlug %d %v", id, err)
	}
	if _, err := s.IDBySlug(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	m, err := s.Matcher(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := m.Match("200 g softened sunflower oil"); !ok || got != oil {
		t.Errorf("alias match %d %v", got, ok)
	}
	if got, ok := m.Match("Сливочное масло"); !ok || got != butter {
		t.Errorf("translated match %d %v", got, ok)
	}
	m2, _ := s.Matcher(ctx)
	if m2 != m {
		t.Error("matcher should be cached")
	}
	if _, err := s.Upsert(ctx, Ingredient{Slug: "egg", Name: "egg"}); err != nil {
		t.Fatal(err)
	}
	m3, _ := s.Matcher(ctx)
	if m3 == m {
		t.Error("matcher should be rebuilt after upsert")
	}
}
