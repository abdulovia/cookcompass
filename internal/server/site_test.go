package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

func TestBrowseFilters(t *testing.T) {
	h := newHarness(t)
	resp, body := h.get("/recipes")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "46 recipes") {
		t.Fatalf("browse all: %d %s", resp.StatusCode, snippet(body, "recipes</p>"))
	}
	tests := []struct {
		query string
		count string
		has   []string
		lacks []string
	}{
		{"f=1&cuisine=italian", "5 recipes", []string{"Spaghetti Carbonara", "Tomato Bruschetta"}, []string{"Pad Thai"}},
		{"f=1&meal=breakfast&difficulty=easy", "", []string{"Fluffy Pancakes", "Syrniki"}, []string{"Beef Bourguignon"}},
		{"f=1&q=curry", "1 recipe", []string{"Thai Green Curry"}, nil},
		{"f=1&difficulty=hard", "8 recipes", []string{"Pelmeni"}, []string{"Guacamole"}},
		{"f=1&cuisine=nope", "0 recipes", []string{"No recipes match"}, nil},
	}
	for _, tc := range tests {
		resp, body := h.get("/recipes?" + tc.query)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %d", tc.query, resp.StatusCode)
			continue
		}
		if tc.count != "" && !strings.Contains(body, ">"+tc.count) {
			t.Errorf("%s: want %q, got %s", tc.query, tc.count, snippet(body, "recipe"))
		}
		for _, s := range tc.has {
			if !strings.Contains(body, s) {
				t.Errorf("%s: missing %q", tc.query, s)
			}
		}
		for _, s := range tc.lacks {
			if strings.Contains(body, s) {
				t.Errorf("%s: unexpected %q", tc.query, s)
			}
		}
	}
	// HTMX requests targeting #results get only the fragment.
	resp, body = h.get("/recipes?f=1&cuisine=thai", "HX-Request", "true", "HX-Target", "results")
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "<html") || !strings.Contains(body, `id="results"`) {
		t.Fatalf("htmx partial: %d", resp.StatusCode)
	}
}

func TestOnlyCookableAndDislikes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	id := func(slug string) string {
		v, err := h.app.Ings.IDBySlug(ctx, slug)
		if err != nil {
			t.Fatal(err)
		}
		return strconv.FormatInt(v, 10)
	}
	// Empty pantry: hint is shown.
	_, body := h.get("/recipes?f=1&cookable=1")
	if !strings.Contains(body, "Tell us what you have at home") {
		t.Fatal("pantry hint missing")
	}
	// Pantry: eggs, butter → omelette (butter→oil substitute aside) is cookable.
	form := url.Values{"pantry": {id("egg"), id("butter"), id("chives")}}
	if resp, _ := h.post("/settings/pantry", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save pantry: %d", resp.StatusCode)
	}
	_, body = h.get("/recipes?f=1&cookable=1")
	if !strings.Contains(body, "French Omelette") || strings.Contains(body, "Pad Thai") {
		t.Fatalf("cookable filter wrong: %s", snippet(body, "recipes</p>"))
	}
	if _, body = h.get("/recipes/french-omelette"); !strings.Contains(body, "At home") {
		t.Fatal("at-home badge missing")
	}

	// Allergy to peanuts hides kung pao everywhere; dislike of tvorog hides syrniki by default only.
	prefs := url.Values{"allergy": {id("peanuts")}, "dislike": {id("tvorog")}}
	if resp, _ := h.post("/settings/preferences", prefs); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save prefs: %d", resp.StatusCode)
	}
	_, body = h.get("/recipes")
	if strings.Contains(body, "Kung Pao") || strings.Contains(body, "Syrniki") {
		t.Fatal("allergen or disliked recipe shown by default")
	}
	_, body = h.get("/recipes?f=1")
	if strings.Contains(body, "Kung Pao") || !strings.Contains(body, "Syrniki") {
		t.Fatal("with hide_disliked off: allergens must stay hidden, dislikes shown")
	}
	if _, body = h.get("/settings"); !strings.Contains(body, `value="`+id("peanuts")+`" checked`) {
		t.Fatal("allergy not pre-checked in settings")
	}
}

func TestRecipeDetailAndSettings(t *testing.T) {
	h := newHarness(t)
	resp, body := h.get("/recipes/spaghetti-carbonara")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail: %d", resp.StatusCode)
	}
	for _, want := range []string{"Spaghetti Carbonara", "Per serving", "610", "More nutrition details", "x-data=\"steps(", "400 g spaghetti",
		"youtube.com/results"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
	if resp, _ := h.get("/recipes/does-not-exist"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing recipe: %d", resp.StatusCode)
	}

	// Embedded video when a concrete YouTube link is set.
	r, _ := h.app.Recipes.GetBySlug(context.Background(), "guacamole")
	r.VideoURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	if err := h.app.Recipes.Update(context.Background(), r, recipesChange()); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/recipes/guacamole"); !strings.Contains(body, "youtube-nocookie.com/embed/dQw4w9WgXcQ") {
		t.Fatal("video not embedded")
	}

	// Settings: switch to imperial, Germany, Berlin.
	cities, _ := h.app.Catalog.Cities(context.Background(), "DE")
	form := url.Values{"country": {"DE"}, "city": {strconv.FormatInt(cities[0].ID, 10)}, "units": {"imperial"}, "lang": {"en"}}
	if resp, _ := h.post("/settings", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save settings: %d", resp.StatusCode)
	}
	prof, _ := h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	if prof.CountryCode != "DE" || prof.CityID != cities[0].ID || prof.Units != "imperial" || prof.Locale != "en" {
		t.Fatalf("settings not saved: %+v", prof.Settings)
	}
	if _, body := h.get("/recipes/spaghetti-carbonara"); !strings.Contains(body, "14 oz spaghetti") {
		t.Fatal("imperial conversion not applied")
	}
	// Changing country resets an incompatible city; unknown values are ignored.
	form = url.Values{"country": {"US"}, "city": {strconv.FormatInt(cities[0].ID, 10)}, "units": {"bogus"}, "lang": {"xx"}}
	h.post("/settings", form)
	prof, _ = h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	if prof.CountryCode != "US" || prof.CityID != 0 || prof.Units != "metric" || prof.Locale != "en" {
		t.Fatalf("invalid settings applied: %+v", prof.Settings)
	}
	h.post("/settings", url.Values{"lang": {""}, "city": {""}})
	if resp, body := h.get("/settings?flash=settings_saved"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "Settings saved") {
		t.Fatal("settings page")
	}
	if resp, _ := h.get("/"); resp.StatusCode != http.StatusOK {
		t.Fatal("home")
	}
}

func TestAPIGolden(t *testing.T) {
	h := newHarness(t)
	_, list := h.get("/api/recipes?f=1&cuisine=italian")
	testutil.GoldenBytes(t, "api_recipes_italian.golden.json", []byte(list))
	_, one := h.get("/api/recipes/spaghetti-carbonara")
	testutil.GoldenBytes(t, "api_recipe_carbonara.golden.json", []byte(one))
	if resp, _ := h.get("/api/recipes/nope"); resp.StatusCode != http.StatusNotFound {
		t.Fatal("api 404")
	}
}

func TestMultiUserAuth(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Auth.MultiUser = true })
	// Guests can browse but not save.
	if resp, _ := h.get("/recipes"); resp.StatusCode != http.StatusOK {
		t.Fatal("guest browse")
	}
	resp, _ := h.post("/settings/pantry", url.Values{})
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("guest save should redirect to login: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, body := h.get("/login"); resp.StatusCode != http.StatusOK || !strings.Contains(body, "Create account") {
		t.Fatal("login page")
	}
	if resp, _ := h.post("/register", url.Values{"username": {"ab"}, "password": {"short"}}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad register: %d", resp.StatusCode)
	}
	if resp, _ := h.post("/register", url.Values{"username": {"anna"}, "password": {"correct horse"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	if resp, _ := h.post("/settings", url.Values{"country": {"RU"}, "units": {"metric"}}); resp.StatusCode != http.StatusSeeOther ||
		!strings.Contains(resp.Header.Get("Location"), "settings_saved") {
		t.Fatalf("logged-in save: %d", resp.StatusCode)
	}
	if _, body := h.get("/"); !strings.Contains(body, "Log out") {
		t.Fatal("logout link missing")
	}
	if resp, _ := h.post("/logout", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("logout")
	}
	if resp, _ := h.post("/register", url.Values{"username": {"anna"}, "password": {"another one"}}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatal("duplicate username accepted")
	}
	if resp, _ := h.post("/login", url.Values{"username": {"anna"}, "password": {"wrong password"}}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("wrong password accepted")
	}
	if resp, _ := h.post("/login", url.Values{"username": {"anna"}, "password": {"correct horse"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("login failed")
	}
	if _, body := h.get("/settings"); !strings.Contains(body, `value="RU" selected`) {
		t.Fatal("settings not restored after login")
	}

	// Single-user mode: auth pages just redirect home.
	single := newHarness(t)
	for _, p := range []string{"/login", "/register"} {
		var resp *http.Response
		if p == "/login" {
			resp, _ = single.get(p)
		} else {
			resp, _ = single.post(p, url.Values{"username": {"x"}})
		}
		if resp.StatusCode != http.StatusSeeOther {
			t.Errorf("%s in single-user mode: %d", p, resp.StatusCode)
		}
	}
	if resp, _ := single.post("/login", url.Values{}); resp.StatusCode != http.StatusSeeOther {
		t.Error("login POST in single-user mode")
	}
}

func snippet(body, near string) string {
	i := strings.Index(body, near)
	if i < 0 {
		if len(body) > 300 {
			return body[:300]
		}
		return body
	}
	return body[max(0, i-200):min(len(body), i+100)]
}
