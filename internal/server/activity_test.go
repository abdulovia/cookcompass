package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/users"
)

// TestFavoritesFlow: the detail page toggles favorites, the browse tab
// filters to them.
func TestFavoritesFlow(t *testing.T) {
	h := newHarness(t)
	// Not a favorite yet.
	if _, body := h.get("/recipes/borscht"); !strings.Contains(body, "☆") {
		t.Fatal("unfavorited state should show an empty star")
	}
	// Toggle on via the form.
	form := url.Values{"favorite": {"1"}}
	if resp, _ := h.post("/recipes/borscht/favorite", form); resp.StatusCode != http.StatusSeeOther &&
		resp.Header.Get("HX-Redirect") == "" && resp.StatusCode != http.StatusOK {
		t.Fatalf("favorite: %d", resp.StatusCode)
	}
	// The browse tab lists it, and only it (plus any other favorites — none).
	if _, body := h.get("/recipes?f=1&fav=1"); !strings.Contains(body, "Borscht") {
		t.Fatalf("favorites tab: %s", snippet(body, "empty"))
	}
	if _, body := h.get("/recipes?f=1&fav=1"); strings.Contains(body, "Pad Thai") {
		t.Fatal("favorites tab should hide the rest")
	}
	// The favorites filter is part of the filter form, so combining it with
	// another filter (an HTMX swap of #results) keeps it.
	_, page := h.get("/recipes?f=1&fav=1")
	if !strings.Contains(page, `name="fav" value="1" checked`) {
		t.Fatal("favorites checkbox should be checked on the favorites view")
	}
	_, body := h.get("/recipes?f=1&fav=1&difficulty=", "HX-Request", "true", "HX-Target", "results")
	if !strings.Contains(body, "Borscht") || strings.Contains(body, "Pad Thai") {
		t.Fatal("favorites filter lost on an HTMX filter change")
	}
	// Full page loads show the action flash once (layout), not twice.
	if _, body := h.get("/recipes/borscht?flash=favorited"); strings.Count(body, "Added to favorites.") != 1 {
		t.Fatalf("flash shown %d times", strings.Count(body, "Added to favorites."))
	}
	// HTMX swaps of the actions card carry their own flash.
	form.Set("favorite", "1")
	if _, body := h.post("/recipes/borscht/favorite", form, "HX-Request", "true", "HX-Target", "recipe-actions"); !strings.Contains(body, "Added to favorites.") {
		t.Fatal("HTMX favorite response lacks its flash")
	}
	// Toggle off.
	form.Set("favorite", "0")
	h.post("/recipes/borscht/favorite", form)
	if _, body := h.get("/recipes?f=1&fav=1"); !strings.Contains(body, "browse.empty") && !strings.Contains(body, "empty") {
		t.Fatal("favorites tab should be empty again")
	}
}

// TestShoppingListFlow: add a recipe's ingredients, check the page, mark
// things bought, "have it" moves an ingredient into the pantry, clear.
func TestShoppingListFlow(t *testing.T) {
	h := newHarness(t)
	if resp, _ := h.post("/recipes/borscht/shopping", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("add: %d", resp.StatusCode)
	}
	items, err := h.app.Users.ShoppingList(context.Background(), h.app.DefaultUserID)
	if err != nil || len(items) == 0 {
		t.Fatalf("items: %d %v", len(items), err)
	}
	// Staples (salt, water…) were skipped.
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Text), "salt") || strings.Contains(strings.ToLower(it.Text), "water") {
			t.Fatalf("staple queued: %q", it.Text)
		}
	}
	// The page shows lines, the recipe name and buy links once a shop is set.
	h.setCountry("RU", true)
	if lenta, err := h.app.Shops.IDBySlug(context.Background(), "RU", "lenta"); err == nil {
		h.post("/settings", url.Values{"country": {"RU"}, "units": {"metric"}, "show_unavailable": {"1"}, "shop": {itoa(lenta)}})
	}
	_, body := h.get("/list")
	if !strings.Contains(body, "borscht") && !strings.Contains(body, "Borscht") {
		t.Fatalf("list page: %s", snippet(body, "shop-list"))
	}
	if !strings.Contains(body, "lenta.com/search") {
		t.Fatal("buy links missing on the list")
	}

	// Mark bought via the toggle (hidden field bought=1).
	first := items[0]
	form := url.Values{"bought": {"1"}}
	if resp, _ := h.post("/list/"+itoa(first.ID)+"/bought", form); resp.StatusCode >= 400 {
		t.Fatalf("bought: %d", resp.StatusCode)
	}
	after, _ := h.app.Users.ShoppingList(context.Background(), h.app.DefaultUserID)
	found := false
	for _, it := range after {
		if it.ID == first.ID {
			found = true
			if !it.Bought {
				t.Fatal("not bought after toggle")
			}
		}
	}
	if !found {
		t.Fatal("row vanished")
	}

	// "Have it" for a line with a linked ingredient adds it to the pantry.
	var withIng users.ShoppingItem
	have := false
	for i := range after {
		if after[i].IngredientID != 0 {
			withIng, have = after[i], true
			break
		}
	}
	if !have {
		t.Fatal("no line with a linked ingredient")
	}
	if resp, _ := h.post("/list/"+itoa(withIng.ID)+"/pantry", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("pantry: %d", resp.StatusCode)
	}
	prof, _ := h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	if !prof.Pantry[withIng.IngredientID] {
		t.Fatal("ingredient not moved to pantry")
	}

	// Clear bought removes checked lines.
	if resp, _ := h.post("/list/clear", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("clear: %d", resp.StatusCode)
	}
	final, _ := h.app.Users.ShoppingList(context.Background(), h.app.DefaultUserID)
	for _, it := range final {
		if it.Bought {
			t.Fatal("bought item survived clear")
		}
	}

	// Plan → list: needs a goal; without one it still redirects.
	if resp, _ := h.post("/goals/shopping", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("plan shopping: %d", resp.StatusCode)
	}
}

// TestCookedWithRating: the cooked form carries a rating and note into the
// history shown on /learn.
func TestCookedWithRating(t *testing.T) {
	h := newHarness(t)
	form := url.Values{"rating": {"5"}, "note": {"Best soup so far"}}
	if resp, _ := h.post("/recipes/borscht/cooked", form); resp.StatusCode >= 400 {
		t.Fatalf("cooked: %d", resp.StatusCode)
	}
	events, err := h.app.Users.RecentCooked(context.Background(), h.app.DefaultUserID, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %d %v", len(events), err)
	}
	if events[0].Rating != 5 || events[0].Note != "Best soup so far" {
		t.Fatalf("event: %+v", events[0])
	}
	if _, body := h.get("/learn"); !strings.Contains(body, "★★★★★") || !strings.Contains(body, "Best soup so far") {
		t.Fatal("learn history should show stars and note")
	}
	// Invalid ratings are dropped rather than rejected.
	h.post("/recipes/borscht/cooked", url.Values{"rating": {"9"}})
	events, _ = h.app.Users.RecentCooked(context.Background(), h.app.DefaultUserID, 10)
	if events[0].Rating != 0 {
		t.Fatalf("rating 9 should clamp to 0, got %d", events[0].Rating)
	}
}

// TestServingsScalerMarkup: ingredient lines carry their original text for
// the client-side scaler.
func TestServingsScalerMarkup(t *testing.T) {
	h := newHarness(t)
	_, body := h.get("/recipes/borscht")
	if !strings.Contains(body, `data-line=`) {
		t.Fatal("ingredient lines should carry data-line")
	}
	if !strings.Contains(body, `x-data="servings(`) {
		t.Fatal("servings component missing")
	}
	if !strings.Contains(body, `name="rating"`) {
		t.Fatal("rating inputs missing")
	}
}
