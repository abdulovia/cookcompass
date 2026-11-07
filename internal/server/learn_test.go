package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/config"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

func TestLearningPathFlow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, body := h.get("/learn"); !strings.Contains(body, "0 of 15 recipes learned") || !strings.Contains(body, "Locked") ||
		!strings.Contains(body, "Nothing yet") {
		t.Fatal("fresh learn page")
	}
	if _, body := h.get("/"); !strings.Contains(body, "Level: Easy — 0 of 5 learned") {
		t.Fatal("home progress")
	}

	// "I cooked this" via HTMX returns the updated action card only.
	resp, body := h.post("/recipes/fluffy-pancakes/cooked", nil, "HX-Request", "true")
	if resp.StatusCode != http.StatusOK || strings.Contains(body, "<html") || !strings.Contains(body, `id="recipe-actions"`) ||
		!strings.Contains(body, "Cooked 1 time") || !strings.Contains(body, "Nice work!") {
		t.Fatalf("htmx cooked: %d %s", resp.StatusCode, body)
	}
	resp, _ = h.post("/recipes/fluffy-pancakes/cooked", nil)
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "flash=cooked") {
		t.Fatalf("cooked redirect: %s", resp.Header.Get("Location"))
	}
	if _, body := h.get("/recipes/fluffy-pancakes"); !strings.Contains(body, "Cooked 2 times") {
		t.Fatal("cooked count on detail")
	}

	// Learn five easy recipes → medium unlocks, next-up suggests medium dishes.
	easy, _ := h.app.Recipes.List(ctx, recipes.Filter{Difficulty: recipes.Easy})
	for _, r := range easy[:5] {
		if resp, _ := h.post("/recipes/"+r.Slug+"/learned", url.Values{"learned": {"1"}}); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("learn %s: %d", r.Slug, resp.StatusCode)
		}
	}
	_, body = h.get("/learn")
	for _, want := range []string{"5 of 15 recipes learned", "Medium", "In progress", "Try next", "Recently cooked", "Fluffy Pancakes"} {
		if !strings.Contains(body, want) {
			t.Errorf("learn page missing %q", want)
		}
	}
	nextUp := body[strings.Index(body, "Try next"):strings.Index(body, "Recently cooked")]
	if !strings.Contains(nextUp, "diff-medium") || strings.Contains(nextUp, "diff-easy") {
		t.Error("next-up should suggest medium recipes only")
	}
	// Undo one: back to easy.
	h.post("/recipes/"+easy[0].Slug+"/learned", url.Values{"learned": {"0"}}, "HX-Request", "true")
	if _, body := h.get("/"); !strings.Contains(body, "Level: Easy — 4 of 5 learned") {
		t.Fatal("unlearn")
	}
	// Soft-deleting a cooked recipe keeps the history.
	pan, _ := h.app.Recipes.GetBySlug(ctx, "fluffy-pancakes")
	if err := h.app.Recipes.SoftDelete(ctx, pan.ID, recipes.Change{}); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/learn"); !strings.Contains(body, "Fluffy Pancakes") {
		t.Fatal("history lost after soft delete")
	}
	if resp, _ := h.post("/recipes/fluffy-pancakes/cooked", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cooking a deleted recipe: %d", resp.StatusCode)
	}
	if resp, _ := h.post("/recipes/nope/learned", url.Values{"learned": {"1"}}); resp.StatusCode != http.StatusNotFound {
		t.Fatal("unknown recipe")
	}
	// Everything learned: the path is complete.
	all, _ := h.app.Recipes.List(ctx, recipes.Filter{})
	for _, r := range all {
		_ = h.app.Users.SetLearned(ctx, h.app.DefaultUserID, r.ID, true)
	}
	if _, body := h.get("/learn"); !strings.Contains(body, "completed the whole path") || !strings.Contains(body, "15 of 15") {
		t.Fatal("complete path")
	}
	// Russian plural forms for the counter.
	if _, body := h.get("/learn", "Cookie", "lang=ru"); !strings.Contains(body, "Освоено 15 из 15 рецептов") {
		t.Fatal("ru learn page")
	}
}

func TestLearningGuestInMultiUser(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Auth.MultiUser = true })
	if _, body := h.get("/recipes/guacamole"); !strings.Contains(body, "log in to track") {
		t.Fatal("guest should be asked to log in")
	}
	if resp, _ := h.post("/recipes/guacamole/cooked", nil); !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatal("guest cooked should redirect to login")
	}
	if _, body := h.get("/learn"); !strings.Contains(body, "0 of 15") {
		t.Fatal("guest learn page")
	}
}
