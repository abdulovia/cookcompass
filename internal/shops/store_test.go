package shops

import (
	"context"
	"errors"
	"testing"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

func TestStoreUpsertAndList(t *testing.T) {
	d := testutil.DB(t)
	ctx := context.Background()
	s := NewStore(d)
	ings := ingredients.NewStore(d)
	if err := catalog.NewStore(d, ings).UpsertCountry(ctx, catalog.Country{Code: "RU", Name: "Russia", Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	sh := &Shop{CountryCode: "RU", Slug: "lenta", Name: "Лента", Website: "https://lenta.com",
		SearchURLTemplate: "https://lenta.com/search/?searchText={q}", Active: true}
	if err := s.Upsert(ctx, sh); err != nil {
		t.Fatal(err)
	}
	sh2 := *sh
	sh2.Name = "Лента (обновлённая)"
	sh2.Active = false
	if err := s.Upsert(ctx, &sh2); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, "RU", false)
	if err != nil || len(list) != 1 || list[0].Name != sh2.Name || list[0].Active {
		t.Fatalf("upsert should update in place: %v %v", list, err)
	}
	// activeOnly skips the deactivated shop
	if list, err = s.List(ctx, "RU", true); err != nil || len(list) != 0 {
		t.Fatalf("activeOnly: %v %v", list, err)
	}
	if list, err = s.List(ctx, "", false); err != nil || len(list) != 1 {
		t.Fatalf("all countries: %v %v", list, err)
	}
	id, err := s.IDBySlug(ctx, "ru", "lenta")
	if err != nil || id != list[0].ID {
		t.Fatalf("IDBySlug: %d %v", id, err)
	}
	if _, err = s.IDBySlug(ctx, "RU", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown slug: %v", err)
	}
}

func TestStoreCRUD(t *testing.T) {
	d := testutil.DB(t)
	ctx := context.Background()
	s := NewStore(d)
	ings := ingredients.NewStore(d)
	if err := catalog.NewStore(d, ings).UpsertCountry(ctx, catalog.Country{Code: "RU", Name: "Russia", Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	sh := &Shop{CountryCode: "RU", Name: "Magnit", Active: true}
	if err := s.Create(ctx, sh); err != nil || sh.ID == 0 || sh.Slug != "magnit" {
		t.Fatalf("create: %v %+v", err, sh)
	}
	if err := s.Create(ctx, &Shop{CountryCode: "RU", Slug: "magnit", Name: "Dup"}); err == nil {
		t.Fatal("duplicate slug should fail")
	}
	sh.SearchURLTemplate = "https://magnit.ru/s?q={q}"
	if err := s.Update(ctx, sh); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, sh.ID)
	if err != nil || got.SearchURLTemplate != sh.SearchURLTemplate {
		t.Fatalf("update not saved: %+v %v", got, err)
	}
	if err := s.Delete(ctx, sh.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, sh.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
	if err := s.Delete(ctx, sh.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete: %v", err)
	}
}

func TestResolver(t *testing.T) {
	d := testutil.DB(t)
	ctx := context.Background()
	ings := ingredients.NewStore(d)
	cat := catalog.NewStore(d, ings)
	sh := NewStore(d)
	if err := cat.UpsertCountry(ctx, catalog.Country{Code: "RU", Name: "Russia", Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	lenta := &Shop{CountryCode: "RU", Slug: "lenta", Name: "Лента", SearchURLTemplate: "https://lenta.com/search/?searchText={q}", Active: true}
	magnit := &Shop{CountryCode: "RU", Slug: "magnit", Name: "Магнит", Active: true}
	for _, x := range []*Shop{lenta, magnit} {
		if err := sh.Create(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	milk := ingredients.Ingredient{Slug: "milk", Name: "Milk", Category: "dairy"}
	if _, err := ings.Upsert(ctx, milk); err != nil {
		t.Fatal(err)
	}
	milkID, err := ings.IDBySlug(ctx, "milk")
	if err != nil {
		t.Fatal(err)
	}
	// A magnit product with an exact page; lenta has none.
	if err := cat.CreateProduct(ctx, &catalog.Product{CountryCode: "RU", SKU: "M-1", Name: "Milk 1L",
		IngredientID: milkID, ShopID: magnit.ID, URL: "https://magnit.ru/p/milk-1l", Price: 89, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Shops: sh, ProductURLs: cat}

	// Shop user + exact product elsewhere: exact wins over own shop search.
	link, ok := r.Link(ctx, "RU", lenta.ID, milkID, "молоко")
	if !ok || !link.Exact || link.URL != "https://magnit.ru/p/milk-1l" {
		t.Fatalf("exact link: %+v %v", link, ok)
	}
	// A generic (shop-less) product outranks another shop's page.
	if err := cat.CreateProduct(ctx, &catalog.Product{CountryCode: "RU", SKU: "G-1", Name: "Milk generic",
		IngredientID: milkID, URL: "https://any.shop/milk", Price: 99, Currency: "RUB"}); err != nil {
		t.Fatal(err)
	}
	link, _ = r.Link(ctx, "RU", lenta.ID, milkID, "молоко")
	if !link.Exact || link.URL != "https://any.shop/milk" {
		t.Fatalf("generic should outrank other shops: %+v", link)
	}
	// Known shop search: lenta's own product page would win, but the shop's
	// own catalog outranking itself is the same query; test search fallback
	// with an ingredient that has no product at all.
	water := ingredients.Ingredient{Slug: "sparkling-water", Name: "Sparkling water", Category: "drinks"}
	if _, err := ings.Upsert(ctx, water); err != nil {
		t.Fatal(err)
	}
	waterID, _ := ings.IDBySlug(ctx, "sparkling-water")
	link, ok = r.Link(ctx, "RU", lenta.ID, waterID, "вода газированная")
	if !ok || link.Exact || link.URL != "https://lenta.com/search/?searchText=%D0%B2%D0%BE%D0%B4%D0%B0+%D0%B3%D0%B0%D0%B7%D0%B8%D1%80%D0%BE%D0%B2%D0%B0%D0%BD%D0%BD%D0%B0%D1%8F" {
		t.Fatalf("search fallback: %+v %v", link, ok)
	}
	// No shop selected and no product page → nothing to offer.
	if _, ok = r.Link(ctx, "RU", 0, waterID, "вода"); ok {
		t.Fatal("no shop selected should not fabricate links")
	}
	// Inactive shop is ignored even when selected.
	if err := sh.Update(ctx, &Shop{ID: magnit.ID, CountryCode: "RU", Slug: "magnit", Name: "Магнит"}); err != nil {
		t.Fatal(err)
	}
	if link, ok = r.Link(ctx, "RU", magnit.ID, waterID, "вода"); ok {
		t.Fatalf("inactive shop: %+v", link)
	}
	// Unknown ingredient id still yields a search link (query-only path).
	link, ok = r.Link(ctx, "RU", lenta.ID, 0, "специя")
	if !ok || link.URL == "" {
		t.Fatalf("unlinked ingredient should search: %+v", link)
	}
}
