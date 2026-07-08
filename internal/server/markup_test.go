package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/abdulovia/cookcompass/internal/config"
)

// nestedForms reports every <form> opened while another is still open.
// Browsers drop such inner tags and let their </form> close the outer form,
// which silently detaches the rest of the outer form's fields.
func nestedForms(body string) []string {
	z := html.NewTokenizer(strings.NewReader(body))
	depth := 0
	var out []string
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken:
			if name, _ := z.TagName(); string(name) == "form" {
				if depth > 0 {
					out = append(out, string(z.Raw()))
				}
				depth++
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "form" && depth > 0 {
				depth--
			}
		}
	}
}

// TestNoNestedForms renders every page with all optional features on (an
// image, a translator, storage-less hints) and rejects nested forms.
func TestNoNestedForms(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Translate.URL = "http://127.0.0.1:1" })
	ctx := context.Background()
	rec, err := h.app.Recipes.GetBySlug(ctx, "pad-thai")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.Recipes.SetImageURL(ctx, rec.ID, "https://example.com/pad-thai.jpg"); err != nil {
		t.Fatal(err)
	}
	editPath := "/admin/recipes/" + strconv.FormatInt(rec.ID, 10) + "/edit"
	paths := []string{
		"/", "/recipes", "/recipes?f=1&fav=1", "/recipes/pad-thai", "/list", "/learn", "/goals", "/settings",
		"/admin", "/admin/recipes", "/admin/recipes/new", editPath, "/admin/products", "/admin/shops", "/admin/queue",
	}
	for _, p := range paths {
		resp, body := h.get(p)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", p, resp.StatusCode)
			continue
		}
		if n := nestedForms(body); len(n) > 0 {
			t.Errorf("%s: nested forms %v", p, n)
		}
	}
	// The edit page's side actions are wired through form="…" attributes.
	_, body := h.get(editPath)
	for _, want := range []string{`id="cache-image-form"`, `id="translate-form"`, `form="translate-form" name="locale"`} {
		if !strings.Contains(body, want) {
			t.Errorf("edit page lacks %s", want)
		}
	}
}
