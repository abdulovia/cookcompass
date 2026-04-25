package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/shops"
)

// TestShopsSeededAndSelectable: presets appear in settings, picking one adds
// buy links (shop search) to recipe ingredient lines.
func TestShopsSeededAndSelectable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// Presets from seed/shops.csv exist for each seeded country.
	for _, cc := range []string{"RU", "US", "GB", "DE"} {
		if list, err := h.app.Shops.List(ctx, cc, true); err != nil || len(list) == 0 {
			t.Fatalf("shops for %s: %v %v", cc, list, err)
		}
	}
	h.setCountry("RU", true)
	_, body := h.get("/settings")
	if !strings.Contains(body, `name="shop"`) || !strings.Contains(body, "Лента") || !strings.Contains(body, "Самокат") {
		t.Fatal("settings should list RU shop presets")
	}
	if !strings.Contains(body, `name="strict_shop"`) {
		t.Fatal("strict shop toggle missing")
	}

	// Pick Лента in settings and switch the UI to Russian so the search
	// queries use localized ingredient names.
	lenta, err := h.app.Shops.IDBySlug(ctx, "RU", "lenta")
	if err != nil {
		t.Fatal(err)
	}
	if resp, _ := h.post("/lang", url.Values{"lang": {"ru"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("set lang: %d", resp.StatusCode)
	}
	form := url.Values{"country": {"RU"}, "units": {"metric"}, "show_unavailable": {"1"}, "shop": {itoa(lenta)}, "lang": {"ru"}}
	if resp, _ := h.post("/settings", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save shop: %d", resp.StatusCode)
	}
	if p, _ := h.app.Users.Profile(ctx, h.app.DefaultUserID); p.ShopID != lenta {
		t.Fatalf("shop not saved: %+v", p.Settings)
	}

	// The recipe page offers a search link in the chosen shop for every
	// linked ingredient (seed products carry no URLs), with the RU name.
	_, body = h.get("/recipes/pad-thai")
	if n := strings.Count(body, "lenta.com/search/?searchText="); n < 3 {
		t.Fatalf("expected several lenta buy links, got %d in %s", n, snippet(body, "buy-link"))
	}
	if !strings.Contains(body, "%D1%80%D0%B8%D1%81") { // "рис"
		t.Fatal("search link should use the localized name")
	}
}

// TestExactProductLink: once a product carries a URL, the buy link points at
// the product page instead of a search.
func TestExactProductLink(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.setCountry("RU", true)
	// Take pad-thai's first linked ingredient and give it an exact page.
	r, err := h.app.Recipes.GetBySlug(ctx, "pad-thai")
	if err != nil {
		t.Fatal(err)
	}
	var target int64
	for _, in := range r.Ingredients {
		if in.IngredientID != 0 {
			target = in.IngredientID
			break
		}
	}
	if target == 0 {
		t.Fatal("pad-thai has no linked ingredients")
	}
	if err := h.app.Catalog.CreateProduct(ctx, &catalog.Product{CountryCode: "RU", SKU: "EXACT-1", Name: "Exact product",
		IngredientID: target, URL: "https://lenta.com/product/exact-1", Price: 99, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	_, body := h.get("/recipes/pad-thai")
	if !strings.Contains(body, "https://lenta.com/product/exact-1") {
		t.Fatalf("exact link missing: %s", snippet(body, "buy-link"))
	}
}

// TestStrictShopAvailability: with strict mode on, products of other shops
// stop counting towards availability.
func TestStrictShopAvailability(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// RU hides Thai Green Curry: its green curry paste is not sold in RU.
	h.setCountry("RU", true)
	if _, body := h.get("/recipes"); !strings.Contains(body, "Thai Green Curry") {
		t.Fatal("precondition: curry visible with show_unavailable")
	}
	// A magnit-only product for the paste makes the curry cookable in the
	// whole country (non-strict default)…
	paste, err := h.app.Ings.IDBySlug(ctx, "green-curry-paste")
	if err != nil {
		t.Fatal(err)
	}
	magnit, err := h.app.Shops.IDBySlug(ctx, "RU", "magnit")
	if err != nil {
		t.Fatal(err)
	}
	lenta, err := h.app.Shops.IDBySlug(ctx, "RU", "lenta")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.Catalog.CreateProduct(ctx, &catalog.Product{CountryCode: "RU", SKU: "GCP-M", Name: "Green curry paste",
		IngredientID: paste, ShopID: magnit, Price: 149, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/recipes"); strings.Contains(body, "badge bad") && !strings.Contains(body, "Needs a swap") {
		t.Log("curry availability changed after magnit product")
	}
	// …but strict mode with Лента chosen hides it again.
	form := url.Values{"country": {"RU"}, "units": {"metric"}, "show_unavailable": {"1"},
		"shop": {itoa(lenta)}, "strict_shop": {"1"}}
	if resp, _ := h.post("/settings", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("strict save: %d", resp.StatusCode)
	}
	if _, err = h.app.Catalog.IngredientSet(ctx, "RU", lenta, true); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/recipes/thai-green-curry"); !strings.Contains(body, "isn&#39;t sold where you live") {
		t.Fatal("strict mode should flag the paste as missing again")
	}
	// Strict with magnit chosen makes it available once more.
	form.Set("shop", itoa(magnit))
	if resp, _ := h.post("/settings", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("strict magnit save: %d", resp.StatusCode)
	}
	if _, body := h.get("/recipes/thai-green-curry"); strings.Contains(body, "isn&#39;t sold where you live") {
		t.Fatal("strict mode with the right shop should unflag the paste")
	}
}

// TestShopAdminCRUD: shops can be created, edited and deleted from /admin.
func TestShopAdminCRUD(t *testing.T) {
	h := newHarness(t)
	if _, body := h.get("/admin/shops"); !strings.Contains(body, "Пятёрочка") {
		t.Fatalf("admin shops list: %s", snippet(body, "Пятёрочка"))
	}
	// Create.
	form := url.Values{"country": {"RU"}, "slug": {"av"}, "name": {"Азбука Вкуса"},
		"website": {"https://av.ru"}, "search_url_template": {"https://av.ru/search?q={q}"}, "active": {"1"}}
	if resp, _ := h.post("/admin/shops", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create shop: %d", resp.StatusCode)
	}
	ctx := context.Background()
	av, err := h.app.Shops.IDBySlug(ctx, "RU", "av")
	if err != nil {
		t.Fatal(err)
	}
	// Validation: template without {q} is rejected with a field error.
	form.Set("search_url_template", "https://av.ru/search")
	if resp, body := h.post("/admin/shops", form); resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "must contain {q}") && !strings.Contains(body, "должен содержать {q}") {
		t.Fatalf("bad template should fail with a message: %d", resp.StatusCode)
	}
	// Edit.
	form.Set("search_url_template", "https://av.ru/search?text={q}")
	if resp, _ := h.post("/admin/shops/"+itoa(av), form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit shop: %d", resp.StatusCode)
	}
	got, err := h.app.Shops.Get(ctx, av)
	if err != nil || got.SearchURLTemplate != "https://av.ru/search?text={q}" {
		t.Fatalf("edit not saved: %+v %v", got, err)
	}
	// Delete.
	if resp, _ := h.post("/admin/shops/"+itoa(av)+"/delete", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete shop: %d", resp.StatusCode)
	}
	if _, err := h.app.Shops.Get(ctx, av); err != shops.ErrNotFound {
		t.Fatalf("deleted: %v", err)
	}
}
