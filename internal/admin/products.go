package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/web"
)

// maxUpload caps catalog uploads.
const maxUpload = 5 << 20

type productForm struct {
	ID         int64
	Country    string
	SKU        string
	Name       string
	Category   string
	Ingredient string
	Shop       string
	URL        string
	Price      string
	Unit       string
	Source     string
	Status     string
	Deleted    bool
}

func (h *Handler) productRoutes(r interface {
	Get(string, http.HandlerFunc)
	Post(string, http.HandlerFunc)
}) {
	r.Get("/products", h.productList)
	r.Get("/products/new", h.productNew)
	r.Post("/products", h.productCreate)
	r.Get("/products/export", h.productExport)
	r.Post("/products/import", h.productImport)
	r.Post("/products/scan", h.productScan)
	r.Get("/products/{id}/edit", h.productEdit)
	r.Post("/products/{id}", h.productUpdate)
	r.Post("/products/{id}/delete", h.productLifecycle(true))
	r.Post("/products/{id}/restore", h.productLifecycle(false))
	r.Post("/queue/products/{id}/approve", h.setProductStatus(catalog.StatusApproved, "approved"))
	r.Post("/queue/products/{id}/reject", h.setProductStatus(catalog.StatusRejected, "rejected"))
}

func (h *Handler) countryParam(r *http.Request) string {
	if c := strings.ToUpper(r.URL.Query().Get("country")); c != "" {
		return c
	}
	return strings.ToUpper(h.App.Cfg.Defaults.Country)
}

func (h *Handler) productList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f := catalog.ProductFilter{Country: h.countryParam(r), Query: q.Get("q"), Unmapped: q.Get("unmapped") == "1"}
	switch q.Get("status") {
	case "", "all":
		f.Statuses = []string{catalog.StatusApproved, catalog.StatusPending, catalog.StatusRejected}
	default:
		f.Statuses = []string{q.Get("status")}
	}
	switch q.Get("deleted") {
	case "include":
		f.Deleted = 1
	case "only":
		f.Deleted = 2
	}
	list, err := h.App.Catalog.ListProducts(ctx, f)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	countries, err := h.App.Catalog.Countries(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := h.page(r, "admin.products")
	p.Data = map[string]any{"Products": list, "Countries": countries, "Country": f.Country}
	h.R.Page(w, http.StatusOK, "admin/products", p)
}

func (h *Handler) renderProductForm(w http.ResponseWriter, r *http.Request, status int, f productForm, errs map[string]string) {
	ctx := r.Context()
	countries, err := h.App.Catalog.Countries(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	ings, err := h.App.Ings.List(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	title := "admin.product_new"
	if f.ID != 0 {
		title = "admin.product_edit"
	}
	p := h.page(r, title)
	p.Errors = errs
	p.Data = map[string]any{"Form": f, "Countries": countries, "Ingredients": ings, "Shops": h.shopOptions(r, f.Country)}
	h.R.Page(w, status, "admin/product_form", p)
}

func (h *Handler) productNew(w http.ResponseWriter, r *http.Request) {
	h.renderProductForm(w, r, http.StatusOK, productForm{Country: h.countryParam(r)}, nil)
}

func (h *Handler) productEdit(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := h.App.Catalog.GetProduct(r.Context(), id)
	if errors.Is(err, catalog.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderProductForm(w, r, http.StatusOK, productForm{ID: p.ID, Country: p.CountryCode, SKU: p.SKU, Name: p.Name,
		Category: p.Category, Ingredient: p.IngredientSlug, Shop: p.ShopSlug, URL: p.URL,
		Price: strconv.FormatFloat(p.Price, 'f', -1, 64), Unit: p.Unit,
		Source: p.Source, Status: p.Status, Deleted: p.Deleted()}, nil)
}

func parseProductForm(r *http.Request) (productForm, *catalog.Product, map[string]string) {
	f := productForm{Country: r.FormValue("country"), SKU: r.FormValue("sku"), Name: r.FormValue("name"),
		Category: r.FormValue("category"), Ingredient: r.FormValue("ingredient"), Shop: r.FormValue("shop"),
		URL: strings.TrimSpace(r.FormValue("url")), Price: r.FormValue("price"), Unit: r.FormValue("unit")}
	errs := map[string]string{}
	p := &catalog.Product{CountryCode: f.Country, SKU: f.SKU, Name: f.Name, Category: strings.TrimSpace(f.Category),
		IngredientSlug: f.Ingredient, ShopSlug: f.Shop, URL: f.URL, Unit: strings.TrimSpace(f.Unit),
		Source: "user", Status: catalog.StatusApproved}
	if s := strings.TrimSpace(f.Price); s != "" {
		v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
		if err != nil {
			errs["price"] = "validation.number"
		}
		p.Price = v
	}
	return f, p, errs
}

func (h *Handler) saveProduct(w http.ResponseWriter, r *http.Request, id int64) {
	f, p, errs := parseProductForm(r)
	f.ID = id
	if len(errs) == 0 {
		var err error
		if id == 0 {
			err = h.App.Catalog.CreateProduct(r.Context(), p)
		} else {
			p.ID = id
			err = h.App.Catalog.UpdateProduct(r.Context(), p)
		}
		var ve catalog.ValidationError
		switch {
		case err == nil:
			web.Redirect(w, r, fmt.Sprintf("/admin/products?country=%s", p.CountryCode), "saved")
			return
		case errors.Is(err, catalog.ErrNotFound):
			http.NotFound(w, r)
			return
		case errors.As(err, &ve):
			for k, v := range ve {
				errs[k] = v
			}
		default:
			h.fail(w, r, err)
			return
		}
	}
	h.renderProductForm(w, r, http.StatusUnprocessableEntity, f, errs)
}

func (h *Handler) productCreate(w http.ResponseWriter, r *http.Request) { h.saveProduct(w, r, 0) }

func (h *Handler) productUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.saveProduct(w, r, id)
}

func (h *Handler) productLifecycle(del bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		fn, flash := h.App.Catalog.RestoreProduct, "restored"
		if del {
			fn, flash = h.App.Catalog.SoftDeleteProduct, "deleted"
		}
		if err := fn(r.Context(), id); errors.Is(err, catalog.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			h.fail(w, r, err)
			return
		}
		back := r.FormValue("back")
		if !strings.HasPrefix(back, "/admin/") {
			back = "/admin/products"
		}
		web.Redirect(w, r, back, flash)
	}
}

func (h *Handler) setProductStatus(status, flash string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := h.App.Catalog.SetProductStatus(r.Context(), id, status); errors.Is(err, catalog.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			h.fail(w, r, err)
			return
		}
		reviewed(w, r, flash)
	}
}

func (h *Handler) productExport(w http.ResponseWriter, r *http.Request) {
	country := h.countryParam(r)
	list, err := h.App.Catalog.ListProducts(r.Context(), catalog.ProductFilter{Country: country})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="products-%s.csv"`, strings.ToLower(country)))
	if err := catalog.WriteCSV(w, list); err != nil {
		h.Log.Error("export products", "err", err)
	}
}

// productImport accepts a CSV or JSON upload for one country.
func (h *Handler) productImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		web.Redirect(w, r, "/admin/products", "import_failed")
		return
	}
	country := strings.ToUpper(r.FormValue("country"))
	file, hdr, err := r.FormFile("file")
	if err != nil {
		web.Redirect(w, r, "/admin/products?country="+country, "import_failed")
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if ext != ".csv" && ext != ".json" {
		web.Redirect(w, r, "/admin/products?country="+country, "import_failed")
		return
	}
	tmp, err := os.CreateTemp("", "catalog-*"+ext)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		h.fail(w, r, err)
		return
	}
	tmp.Close()
	res, err := h.App.ImportProductsFile(r.Context(), tmp.Name(), country)
	if err != nil || len(res.Errors) > 0 {
		h.Log.Warn("product import", "err", err, "row_errors", len(res.Errors))
		web.Redirect(w, r, "/admin/products?country="+country, "import_failed")
		return
	}
	web.Redirect(w, r, "/admin/products?country="+country, "imported")
}

func (h *Handler) productScan(w http.ResponseWriter, r *http.Request) {
	country := r.FormValue("country")
	if _, err := h.App.ScanCatalog(r.Context(), country); err != nil {
		h.Log.Warn("scan from admin", "err", err)
		web.Redirect(w, r, "/admin/queue", "scrape_failed")
		return
	}
	web.Redirect(w, r, "/admin/queue", "scanned")
}

// productQueueData lists pending products for the review queue.
func (h *Handler) productQueueData(ctx context.Context) map[string]any {
	list, err := h.App.Catalog.ListProducts(ctx, catalog.ProductFilter{Statuses: []string{catalog.StatusPending}})
	if err != nil {
		h.Log.Error("pending products", "err", err)
	}
	countries, _ := h.App.Catalog.Countries(ctx)
	return map[string]any{"PendingProducts": list, "Countries": countries}
}
