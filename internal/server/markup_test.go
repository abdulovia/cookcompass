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

// TestServingScalerHints: composed ingredient lines carry the base quantity,
// the text after the unit and the unit's plural forms; units read singular
// for one ("1 handful") on the server too.
func TestServingScalerHints(t *testing.T) {
	h := newHarness(t)
	_, body := h.get("/recipes/aglio-e-olio")
	for _, want := range []string{
		`data-line="4 cloves garlic, sliced paper-thin" data-qty="4" data-rest="garlic, sliced paper-thin"`,
		`&#34;1&#34;:&#34;clove&#34;`, `&#34;2&#34;:&#34;cloves&#34;`,
		`>1 handful parsley, finely chopped<`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("recipe page lacks %s", want)
		}
	}
	// Hand-written Russian lines have no structured quantity: the client
	// falls back to scaling the leading number of the text.
	_, body = h.get("/recipes/aglio-e-olio", "Cookie", "lang=ru")
	if !strings.Contains(body, `data-line="4 зубчика чеснока`) || strings.Contains(body, "data-qty=") {
		t.Fatal("ru hand-written lines should render without scaler hints")
	}
}
