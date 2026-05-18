package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/config"
)

func (h *harness) setCountry(cc string, showUnavailable bool) {
	h.t.Helper()
	form := url.Values{"country": {cc}, "units": {"metric"}}
	if showUnavailable {
		form.Set("show_unavailable", "1")
	}
	if resp, _ := h.post("/settings", form); resp.StatusCode != http.StatusSeeOther {
		h.t.Fatalf("set country: %d", resp.StatusCode)
	}
}

func TestCountryAvailabilityAndSubstitutions(t *testing.T) {
	h := newHarness(t)
	h.setCountry("RU", false)
	_, body := h.get("/recipes")
	for _, hidden := range []string{"Thai Green Curry", "Pork Gyoza", "Bibimbap"} {
		if strings.Contains(body, hidden) {
			t.Errorf("RU should hide %s", hidden)
		}
	}
	if !strings.Contains(body, ">42 recipes") || !strings.Contains(body, "Pad Thai") || !strings.Contains(body, "Needs a swap") {
		t.Fatalf("RU browse: %s", snippet(body, "recipes</p>"))
	}
	_, body = h.get("/recipes/pad-thai")
	if !strings.Contains(body, "No fish sauce nearby? Use soy sauce (1:1).") || !strings.Contains(body, ">Swap<") {
		t.Fatal("substitution note missing on pad thai")
	}
	// Unavailable recipes are still reachable directly and explain themselves.
	_, body = h.get("/recipes/thai-green-curry")
	if !strings.Contains(body, "isn&#39;t sold where you live") || !strings.Contains(body, "Not sold nearby") {
		t.Fatal("green curry page should flag the missing paste")
	}
	// Toggle on: everything is listed, unavailable ones are badged.
	h.setCountry("RU", true)
	if _, body = h.get("/recipes"); !strings.Contains(body, ">46 recipes") || !strings.Contains(body, "Not sold nearby") {
		t.Fatalf("show unavailable: %s", snippet(body, "recipes</p>"))
	}
	if _, body = h.get("/settings"); !strings.Contains(body, `name="show_unavailable" value="1" checked`) {
		t.Fatal("toggle state not shown in settings")
	}
	// The JSON API reflects availability too.
	if _, body = h.get("/api/recipes/thai-green-curry"); !strings.Contains(body, `"available": false`) || !strings.Contains(body, `"status": "missing"`) {
		t.Fatal("api availability")
	}
}

func TestCatalogScanApproveChangesAvailability(t *testing.T) {
	shop, err := os.ReadFile(filepath.Join("..", "catalogscan", "testdata", "shop_ru.html"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("User-agent: *\nAllow: /\n")) })
	mux.HandleFunc("/asian", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(shop) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	targets := filepath.Join(t.TempDir(), "catalog_targets.txt")
	if err := os.WriteFile(targets, []byte("RU "+srv.URL+"/asian\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(c *config.Config) {
		c.Scrape.CatalogFile = targets
		c.Scrape.MinInterval = time.Millisecond
		c.Scrape.InboxDir = t.TempDir()
	})
	h.setCountry("RU", false)
	if _, body := h.get("/recipes"); strings.Contains(body, "Thai Green Curry") {
		t.Fatal("curry visible before scan")
	}

	// Re-scan from settings → items wait for review → curry still hidden.
	resp, _ := h.post("/settings/rescan", nil)
	if !strings.Contains(resp.Header.Get("Location"), "scanned") {
		t.Fatalf("rescan: %s", resp.Header.Get("Location"))
	}
	if _, body := h.get("/recipes"); strings.Contains(body, "Thai Green Curry") {
		t.Fatal("pending products must not change availability")
	}
	_, body := h.get("/admin/queue")
	if !strings.Contains(body, "3 products waiting") || !strings.Contains(body, "green-curry-paste") || !strings.Contains(body, "Not linked") {
		t.Fatalf("product queue: %s", snippet(body, "products waiting"))
	}
	gcp, err := h.app.Catalog.FindProduct(context.Background(), "RU", catalog.ProductKey("", "SHOP-GCP-110", ""))
	if err != nil {
		t.Fatal(err)
	}
	if resp, b := h.post("/admin/queue/products/"+itoa(gcp.ID)+"/approve", nil, "HX-Request", "true"); resp.StatusCode != http.StatusOK || b != "" {
		t.Fatalf("approve product: %d", resp.StatusCode)
	}
	// Approved: green curry paste is now sold in RU → the recipe appears.
	if _, body := h.get("/recipes"); !strings.Contains(body, "Thai Green Curry") {
		t.Fatal("curry should be available after approving the paste")
	}
	// Fish sauce from the shop removes the need for a swap once approved.
	fish, _ := h.app.Catalog.FindProduct(context.Background(), "RU", catalog.ProductKey("", "4600000000017", ""))
	if resp, _ := h.post("/admin/queue/products/"+itoa(fish.ID)+"/reject", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("reject product")
	}
	if _, body := h.get("/recipes/pad-thai"); !strings.Contains(body, "No fish sauce nearby") {
		t.Fatal("rejected product must not count")
	}
	for _, p := range []string{"/admin/queue/products/999999/approve", "/admin/queue/products/x/reject"} {
		if resp, _ := h.post(p, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, resp.StatusCode)
		}
	}
	if resp, _ := h.post("/admin/products/scan", url.Values{"country": {"all"}}); !strings.Contains(resp.Header.Get("Location"), "scanned") {
		t.Fatal("admin scan all")
	}
	if resp, _ := h.post("/admin/products/scan", url.Values{"country": {"ZZ"}}); !strings.Contains(resp.Header.Get("Location"), "scrape_failed") {
		t.Fatal("admin scan unknown country")
	}
}

func multipartBody(t *testing.T, fields map[string]string, file, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if file != "" {
		fw, _ := mw.CreateFormFile("file", file)
		_, _ = fw.Write([]byte(content))
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestProductAdminCRUDAndCSV(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	// Create with validation.
	if resp, body := h.post("/admin/products", url.Values{"country": {"DE"}, "name": {""}, "price": {"x"}}); resp.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "Please enter a number") {
		t.Fatalf("validation: %d", resp.StatusCode)
	}
	form := url.Values{"country": {"DE"}, "sku": {"DE-MY-TAHINI"}, "name": {"Tahini, 300 g"}, "category": {"pantry"}, "price": {"4,99"}, "unit": {"300 g"}}
	if resp, _ := h.post("/admin/products", form); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	p, err := h.app.Catalog.FindProduct(ctx, "DE", catalog.ProductKey("", "DE-MY-TAHINI", ""))
	if err != nil || p.Price != 4.99 || p.IngredientSlug != "tahini" || p.Source != "user" {
		t.Fatalf("created product: %+v %v", p, err)
	}
	if resp, _ := h.post("/admin/products", form); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatal("duplicate SKU accepted")
	}
	// Edit, delete, restore.
	form.Set("price", "5.49")
	form.Set("ingredient", "peanut-butter")
	if resp, _ := h.post("/admin/products/"+itoa(p.ID), form); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("update")
	}
	if p, _ = h.app.Catalog.GetProduct(ctx, p.ID); p.Price != 5.49 || p.IngredientSlug != "peanut-butter" {
		t.Fatalf("updated: %+v", p)
	}
	if resp, _ := h.post("/admin/products/"+itoa(p.ID)+"/delete", url.Values{"back": {"/admin/products?country=DE"}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("delete")
	}
	if _, body := h.get("/admin/products?country=DE&deleted=only"); !strings.Contains(body, "Tahini, 300 g") {
		t.Fatal("trash")
	}
	if resp, _ := h.post("/admin/products/"+itoa(p.ID)+"/restore", nil); resp.StatusCode != http.StatusSeeOther {
		t.Fatal("restore")
	}
	for _, path := range []string{"/admin/products/999999/edit", "/admin/products/x/edit"} {
		if resp, _ := h.get(path); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
	}
	for _, path := range []string{"/admin/products/999999", "/admin/products/999999/delete", "/admin/products/x/restore", "/admin/products/x"} {
		if resp, _ := h.post(path, form); resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s: %d", path, resp.StatusCode)
		}
	}
	for _, path := range []string{"/admin/products?country=DE&q=tahini&status=approved&unmapped=1&deleted=include", "/admin/products/new?country=RU",
		"/admin/products/" + itoa(p.ID) + "/edit", "/admin/products"} {
		if resp, _ := h.get(path); resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %d", path, resp.StatusCode)
		}
	}

	// CSV export → edit → import round trip.
	resp, csv := h.get("/admin/products/export?country=DE")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(csv, "sku,name,category,ingredient,shop,url,price,unit\n") ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "products-de.csv") {
		t.Fatalf("export: %d %s", resp.StatusCode, resp.Header.Get("Content-Disposition"))
	}
	edited := strings.Replace(csv, "DE-MY-TAHINI,\"Tahini, 300 g\",pantry,peanut-butter,,,5.49", "DE-MY-TAHINI,\"Tahini, 300 g\",pantry,tahini,,,3.99", 1) +
		"DE-NEW-1,Gochujang 500 g,condiments,gochujang,,,6.99,500 g\n"
	body, ctype := multipartBody(t, map[string]string{"country": "DE"}, "de.csv", edited)
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/admin/products/import", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Origin", h.srv.URL)
	if resp, _ := h.do(req); !strings.Contains(resp.Header.Get("Location"), "flash=imported") {
		t.Fatalf("import: %s", resp.Header.Get("Location"))
	}
	if p, _ = h.app.Catalog.GetProduct(ctx, p.ID); p.Price != 3.99 || p.IngredientSlug != "tahini" {
		t.Fatalf("import did not update: %+v", p)
	}
	if _, err := h.app.Catalog.FindProduct(ctx, "DE", catalog.ProductKey("", "DE-NEW-1", "")); err != nil {
		t.Fatal("import did not create")
	}
	// With gochujang now sold in DE, bibimbap no longer needs the sriracha swap.
	h.setCountry("DE", false)
	if _, body := h.get("/recipes/bibimbap"); strings.Contains(body, "No gochujang nearby") {
		t.Fatal("imported product should remove the substitution")
	}

	// JSON upload and bad uploads.
	example, _ := os.ReadFile("../../docs/examples/catalog.json")
	body, ctype = multipartBody(t, map[string]string{"country": "RU"}, "shop.json", string(example))
	req, _ = http.NewRequest(http.MethodPost, h.srv.URL+"/admin/products/import", body)
	req.Header.Set("Content-Type", ctype)
	if resp, _ := h.do(req); !strings.Contains(resp.Header.Get("Location"), "flash=imported") {
		t.Fatalf("json import: %s", resp.Header.Get("Location"))
	}
	for _, tc := range []struct{ name, content string }{{"x.txt", "hello"}, {"bad.csv", "sku\nx\n"}, {"", ""}} {
		body, ctype = multipartBody(t, map[string]string{"country": "RU"}, tc.name, tc.content)
		req, _ = http.NewRequest(http.MethodPost, h.srv.URL+"/admin/products/import", body)
		req.Header.Set("Content-Type", ctype)
		if resp, _ := h.do(req); !strings.Contains(resp.Header.Get("Location"), "import_failed") {
			t.Errorf("%q: %s", tc.name, resp.Header.Get("Location"))
		}
	}
	req, _ = http.NewRequest(http.MethodPost, h.srv.URL+"/admin/products/import", strings.NewReader("x"))
	req.Header.Set("Content-Type", "text/plain")
	if resp, _ := h.do(req); !strings.Contains(resp.Header.Get("Location"), "import_failed") {
		t.Error("non-multipart import")
	}
}

func TestGeolocation(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.post("/settings/locate", url.Values{"lat": {"52.52"}, "lng": {"13.40"}})
	if !strings.Contains(resp.Header.Get("Location"), "location_detected") {
		t.Fatalf("locate Berlin: %s", resp.Header.Get("Location"))
	}
	prof, _ := h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	city, _ := h.app.Catalog.City(context.Background(), prof.CityID)
	if prof.CountryCode != "DE" || city.Name != "Berlin" {
		t.Fatalf("located %s/%s", prof.CountryCode, city.Name)
	}
	for _, f := range []url.Values{{"lat": {"0"}, "lng": {"-160"}}, {"lat": {"x"}, "lng": {"1"}}, {"lat": {"200"}, "lng": {"1"}}} {
		if resp, _ := h.post("/settings/locate", f); !strings.Contains(resp.Header.Get("Location"), "location_failed") {
			t.Errorf("%v: %s", f, resp.Header.Get("Location"))
		}
	}
	prof, _ = h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	if prof.CountryCode != "DE" {
		t.Fatal("failed detection must keep the previous country")
	}
	if _, body := h.get("/settings?flash=location_detected"); !strings.Contains(body, "Location detected") || !strings.Contains(body, "Use my location") {
		t.Fatal("settings page")
	}
}
