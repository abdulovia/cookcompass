package users

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/abdulovia/cookcompass/internal/testutil"
)

func TestUsersStore(t *testing.T) {
	ctx := context.Background()
	d := testutil.DB(t)
	s := NewStore(d)
	for _, q := range []string{
		`INSERT INTO countries (code, name, currency, lat, lng) VALUES ('US', 'US', 'USD', 0, 0)`,
		`INSERT INTO ingredients (id, slug, name) VALUES (1, 'egg', 'egg'), (2, 'milk', 'milk')`,
		`INSERT INTO recipes (id, slug, name, difficulty, dedup_key) VALUES (1, 'a', 'A', 'easy', 'k')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.EnsureDefault(ctx, "us")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := s.EnsureDefault(ctx, "US"); again != id {
		t.Fatal("EnsureDefault must be idempotent")
	}
	p, err := s.Profile(ctx, id)
	if err != nil || p.Username != DefaultUsername || p.CountryCode != "US" || p.Units != "metric" || p.HasGoal() {
		t.Fatalf("profile %+v %v", p, err)
	}
	if _, err := s.Profile(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing profile")
	}

	st := p.Settings
	st.Units, st.HeightCm, st.WeightKg, st.Age, st.Sex, st.Activity, st.Goal = "imperial", 180, 80, 30, "male", "light", "maintain"
	if err := s.SaveSettings(ctx, id, st); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(ctx, 999, st); !errors.Is(err, ErrNotFound) {
		t.Fatal("save missing user")
	}
	if err := s.SetPreferences(ctx, id, map[int64]string{1: Allergy, 2: "whatever"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPantry(ctx, id, []int64{1, 1, 2}); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Profile(ctx, id)
	if !p.HasGoal() || p.Units != "imperial" || p.Prefs[1] != Allergy || p.Prefs[2] != Dislike || len(p.Pantry) != 2 {
		t.Fatalf("profile after updates %+v", p)
	}
	if len(p.Excluded(false)) != 1 || len(p.Excluded(true)) != 2 {
		t.Fatal("excluded")
	}

	// Accounts and sessions.
	if _, err := s.Create(ctx, "ab", "password1", ""); err == nil {
		t.Fatal("short username")
	}
	if _, err := s.Create(ctx, "anna", "short", ""); err == nil {
		t.Fatal("short password")
	}
	uid, err := s.Create(ctx, " Anna ", "correct horse", "US")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, "anna", "another pass", ""); !errors.Is(err, ErrUsernameUsed) {
		t.Fatalf("duplicate: %v", err)
	}
	if got, err := s.Authenticate(ctx, "ANNA", "correct horse"); err != nil || got != uid {
		t.Fatalf("auth %d %v", got, err)
	}
	for _, c := range [][2]string{{"anna", "wrong"}, {"nobody", "x"}, {DefaultUsername, ""}} {
		if _, err := s.Authenticate(ctx, c[0], c[1]); !errors.Is(err, ErrBadLogin) {
			t.Errorf("%v: %v", c, err)
		}
	}
	tok, err := s.NewSession(ctx, uid)
	if err != nil || len(tok) != 64 {
		t.Fatal(err)
	}
	if got, err := s.SessionUser(ctx, tok); err != nil || got != uid {
		t.Fatal("session lookup")
	}
	if _, err := d.Exec(`UPDATE sessions SET expires_at = '2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, tok); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}

	// Cooking history and learned.
	for i := 0; i < 3; i++ {
		if err := s.LogCooked(ctx, id, 1, 0, ""); err != nil {
			t.Fatal(err)
		}
	}
	counts, _ := s.CookedCounts(ctx, id)
	recent, _ := s.RecentCooked(ctx, id, 2)
	if counts[1] != 3 || len(recent) != 2 || recent[0].At.IsZero() {
		t.Fatalf("cooked %v %v", counts, recent)
	}
	// Notes are capped server-side (the form's maxlength is only a hint).
	if err := s.LogCooked(ctx, id, 1, 5, strings.Repeat("щ", MaxNoteRunes+50)); err != nil {
		t.Fatal(err)
	}
	if recent, _ := s.RecentCooked(ctx, id, 1); len(recent) != 1 || utf8.RuneCountInString(recent[0].Note) != MaxNoteRunes {
		t.Fatalf("note not capped: %+v", recent)
	}
	if err := s.SetLearned(ctx, id, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLearned(ctx, id, 1, true); err != nil {
		t.Fatal("learning twice must be a no-op")
	}
	if l, _ := s.Learned(ctx, id); len(l) != 1 {
		t.Fatal("learned")
	}
	_ = s.SetLearned(ctx, id, 1, false)
	if l, _ := s.Learned(ctx, id); len(l) != 0 {
		t.Fatal("unlearn")
	}
}
