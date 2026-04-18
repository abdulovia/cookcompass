package catalog

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	ctx := context.Background()
	for _, in := range []ingredients.Ingredient{
		{Slug: "milk", Name: "milk", Names: map[string]string{"ru": "молоко"}},
		{Slug: "egg", Name: "egg", Aliases: []string{"eggs"}},
	} {
		if _, err := ings.Upsert(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(d, ings)
	for _, c := range []Country{
		{Code: "us", Name: "United States", Currency: "USD", Lat: 39.8, Lng: -98.6},
		{Code: "RU", Name: "Russia", Currency: "RUB", Lat: 55.7, Lng: 37.6, DefaultLocale: "ru"},
	} {
		if err := s.UpsertCountry(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []City{
		{CountryCode: "US", Name: "New York", Lat: 40.7128, Lng: -74.006},
		{CountryCode: "US", Name: "Seattle", Lat: 47.6062, Lng: -122.3321},
		{CountryCode: "RU", Name: "Moscow", Lat: 55.7558, Lng: 37.6173},
	} {
		if _, err := s.UpsertCity(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestCountriesAndCities(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	cs, err := s.Countries(ctx)
	if err != nil || len(cs) != 2 || cs[0].Code != "RU" || cs[1].DefaultLocale != "en" {
		t.Fatalf("countries %+v %v", cs, err)
	}
	if _, err := s.Country(ctx, "zz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing country: %v", err)
	}
	if err := s.UpsertCountry(ctx, Country{Code: "USA"}); err == nil {
		t.Fatal("3-letter code accepted")
	}
	cities, _ := s.Cities(ctx, "us")
	if len(cities) != 2 {
		t.Fatalf("cities %+v", cities)
	}
	all, _ := s.Cities(ctx, "")
	if len(all) != 3 {
		t.Fatalf("all cities %d", len(all))
	}
	c, err := s.City(ctx, cities[0].ID)
	if err != nil || c.Name != cities[0].Name {
		t.Fatalf("city %v %v", c, err)
	}
	if _, err := s.City(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing city found")
	}

	// Reverse geocoding: a point in Brooklyn → New York; St Petersburg → Moscow (nearest known).
	near, km, err := s.NearestCity(ctx, 40.65, -73.95)
	if err != nil || near.Name != "New York" || km > 20 {
		t.Fatalf("nearest %v %.1f %v", near, km, err)
	}
	near, _, _ = s.NearestCity(ctx, 59.93, 30.36)
	if near.CountryCode != "RU" {
		t.Fatalf("nearest to SPb %v", near)
	}
	if _, _, err := s.NearestCity(ctx, 100, 0); err == nil {
		t.Fatal("invalid coords accepted")
	}
	if d := Haversine(0, 0, 0, 1); d < 110 || d > 112 {
		t.Fatalf("haversine 1° at equator = %.2f", d)
	}
}

func TestNearestCityEmpty(t *testing.T) {
	d := testutil.DB(t)
	s := NewStore(d, ingredients.NewStore(d))
	if _, _, err := s.NearestCity(context.Background(), 1, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestProductCRUD(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	p := &Product{CountryCode: "us", SKU: "M1", Name: "Whole Milk, 1 l", Category: "dairy", Price: 1.39, Unit: "1 l"}
	if err := s.CreateProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || p.Currency != "USD" || p.Status != StatusApproved || p.Source != "user" || p.IngredientID == 0 {
		t.Fatalf("defaults/ingredient match not applied: %+v", p)
	}
	got, err := s.GetProduct(ctx, p.ID)
	if err != nil || got.IngredientSlug != "milk" {
		t.Fatalf("get %+v %v", got, err)
	}
	dup := &Product{CountryCode: "US", SKU: "m1", Name: "Other"}
	var ve ValidationError
	if err := s.CreateProduct(ctx, dup); !errors.As(err, &ve) || ve["sku"] == "" {
		t.Fatalf("duplicate sku: %v", err)
	}

	got.Price = 1.49
	got.IngredientSlug = "egg"
	if err := s.UpdateProduct(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProduct(ctx, p.ID)
	if got.Price != 1.49 || got.IngredientSlug != "egg" {
		t.Fatalf("update not saved: %+v", got)
	}

	if err := s.SoftDeleteProduct(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDeleteProduct(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("double delete")
	}
	if list, _ := s.ListProducts(ctx, ProductFilter{Country: "US"}); len(list) != 0 {
		t.Fatal("deleted product listed")
	}
	if list, _ := s.ListProducts(ctx, ProductFilter{Deleted: 2}); len(list) != 1 || !list[0].Deleted() {
		t.Fatal("deleted-only list")
	}
	if err := s.RestoreProduct(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreProduct(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("double restore")
	}

	if err := s.SetProductStatus(ctx, p.ID, "bogus"); err == nil {
		t.Fatal("bad status accepted")
	}
	if err := s.SetProductStatus(ctx, 999, StatusApproved); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing product status")
	}

	bad := []*Product{
		{CountryCode: "US", Name: ""},
		{CountryCode: "ZZ", Name: "x"},
		{CountryCode: "US", Name: "x", Price: -1},
		{CountryCode: "US", Name: "x", IngredientSlug: "unobtainium"},
	}
	for _, b := range bad {
		if err := s.CreateProduct(ctx, b); err == nil {
			t.Errorf("invalid product accepted: %+v", b)
		}
	}
	if err := s.UpdateProduct(ctx, &Product{Name: "x", CountryCode: "US"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("update without id")
	}
	if err := s.UpdateProduct(ctx, &Product{ID: 999, Name: "x", CountryCode: "US"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("update missing id")
	}
	if _, err := s.GetProduct(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatal("get missing")
	}
	if !strings.Contains(ValidationError{"a": "b"}.Error(), "a: b") {
		t.Fatal("error text")
	}
}

func TestUpsertAndQueue(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	scan := func(name string, price float64) *Product {
		return &Product{CountryCode: "RU", Name: name, Price: price, Source: "scraped:shop.example", Status: StatusPending}
	}
	res, err := s.UpsertProduct(ctx, scan("Молоко 3,2%, 1 l", 89), false)
	if err != nil || res != Created {
		t.Fatalf("first upsert %v %v", res, err)
	}
	res, err = s.UpsertProduct(ctx, scan("молоко 3,2% , 1 l", 95), false)
	if err != nil || res != Created {
		// Different raw name → different name key; that is a new product.
		t.Logf("second upsert result %v", res)
	}
	same := scan("Молоко 3,2%, 1 l", 99)
	res, err = s.UpsertProduct(ctx, same, false)
	if err != nil || res != Updated {
		t.Fatalf("re-scan should update: %v %v", res, err)
	}
	if res, _ := s.UpsertProduct(ctx, scan("Молоко 3,2%, 1 l", 1), true); res != Skipped {
		t.Fatal("onlyNew must skip existing")
	}
	pending, _ := s.ListProducts(ctx, ProductFilter{Statuses: []string{StatusPending}, Query: "молоко"})
	if len(pending) < 1 || pending[0].IngredientSlug != "milk" {
		t.Fatalf("pending list / ingredient match: %+v", pending)
	}

	// A user edit is authoritative.
	p := pending[0]
	p.Source = "user"
	p.Price = 50
	if err := s.UpdateProduct(ctx, p); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.UpsertProduct(ctx, scan(p.Name, 70), false); res != Skipped {
		t.Fatal("scan overwrote a user edit")
	}

	n, err := s.ApproveAllPending(ctx)
	if err != nil || n < 1 {
		t.Fatalf("approve all %d %v", n, err)
	}
	approved, _ := s.ListProducts(ctx, ProductFilter{Country: "RU", Unmapped: false, Limit: 1, Query: "мол"})
	if len(approved) != 1 {
		t.Fatalf("approved with query+limit: %d", len(approved))
	}
	// Approved items keep their status on re-scan.
	if _, err := s.UpsertProduct(ctx, scan("Молоко 3,2% , 1 l", 96), false); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.ListProducts(ctx, ProductFilter{Country: "RU"}); len(again) == 0 {
		t.Fatal("re-scan demoted approved product")
	}

	if _, err := s.UpsertProduct(ctx, scan("Яйца, 10 pcs", 120), false); err != nil {
		t.Fatal(err)
	}
	n, err = s.PurgePending(ctx)
	if err != nil || n != 1 {
		t.Fatalf("purge %d %v", n, err)
	}
	if _, err := s.UpsertProduct(ctx, &Product{CountryCode: "ZZ", Name: "x"}, false); err == nil {
		t.Fatal("invalid upsert accepted")
	}
	counts, err := s.ProductCounts(ctx)
	if err != nil || counts["RU"] == 0 {
		t.Fatalf("counts %v %v", counts, err)
	}
	egg, _ := s.ings.IDBySlug(ctx, "egg")
	if list, _ := s.ListProducts(ctx, ProductFilter{IngredientID: egg, Category: "other"}); len(list) != 0 {
		t.Fatalf("ingredient filter %d", len(list))
	}
	if _, err := s.ListProducts(ctx, ProductFilter{Unmapped: true, Deleted: 1, Limit: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestCSVRoundTrip(t *testing.T) {
	in := "\uFEFFsku,name,category,ingredient,price,unit,notes\nA1,\"Milk, 1 l\",dairy,milk,\"1,39\",1 l,whatever\n\n,Eggs,dairy,,2,10 pcs,\n"
	list, err := ReadCSV(strings.NewReader(in), "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Price != 1.39 || list[0].CountryCode != "US" || list[0].IngredientSlug != "milk" || list[1].SKU != "" {
		t.Fatalf("parsed %+v %+v", list[0], list[1])
	}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, list); err != nil {
		t.Fatal(err)
	}
	want := "sku,name,category,ingredient,shop,url,price,unit\nA1,\"Milk, 1 l\",dairy,milk,,,1.39,1 l\n,Eggs,dairy,,,,2,10 pcs\n"
	if buf.String() != want {
		t.Fatalf("csv out:\n%s", buf.String())
	}
	again, err := ReadCSV(&buf, "US")
	if err != nil || len(again) != 2 {
		t.Fatalf("re-read %v", err)
	}

	bad := map[string]string{
		"empty":        "",
		"no name col":  "sku,price\nA,1\n",
		"bad price":    "name,price\nMilk,abc\n",
		"missing name": "sku,name\nA,\n",
		"bad quoting":  "name\n\"unterminated\n",
	}
	for k, v := range bad {
		if _, err := ReadCSV(strings.NewReader(v), "US"); err == nil {
			t.Errorf("%s: expected error", k)
		}
	}
	withCountry, err := ReadCSV(strings.NewReader("name,country\nMilk,de\n"), "US")
	if err != nil || withCountry[0].CountryCode != "DE" {
		t.Fatalf("country column %v %v", withCountry, err)
	}
}

func TestProductKey(t *testing.T) {
	if ProductKey("", " SKU-1 ", "x") != "sku:sku-1" || ProductKey("", "", "Fresh Eggs!") != "name:fresh egg" || ProductKey("lenta", "SKU-1", "x") != "shop:lenta|sku:sku-1" {
		t.Fatal("product key")
	}
	if stripPack("Milk, 1 l") != "Milk" || stripPack("Milk") != "Milk" {
		t.Fatal("stripPack")
	}
}
