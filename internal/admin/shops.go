package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/web"
)

func (h *Handler) shopRoutes(r interface {
	Get(string, http.HandlerFunc)
	Post(string, http.HandlerFunc)
}) {
	r.Get("/shops", h.shopList)
	r.Get("/shops/new", h.shopNew)
	r.Post("/shops", h.shopCreate)
	r.Get("/shops/{id}/edit", h.shopEdit)
	r.Post("/shops/{id}", h.shopUpdate)
	r.Post("/shops/{id}/delete", h.shopDelete)
}

type shopForm struct {
	ID        int64
	Country   string
	Slug      string
	Name      string
	Website   string
	SearchURL string
	Active    bool
}

func (h *Handler) shopList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := h.App.Shops.List(ctx, "", false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	countries, err := h.App.Catalog.Countries(ctx)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := h.page(r, "admin.shops")
	p.Data = map[string]any{"Shops": list, "Countries": countries, "Country": h.countryParam(r)}
	h.R.Page(w, http.StatusOK, "admin/shops", p)
}

func (h *Handler) renderShopForm(w http.ResponseWriter, r *http.Request, status int, f shopForm, errs map[string]string) {
	countries, err := h.App.Catalog.Countries(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	title := "admin.shop_new"
	if f.ID != 0 {
		title = "admin.shop_edit"
	}
	p := h.page(r, title)
	p.Errors = errs
	p.Data = map[string]any{"Form": f, "Countries": countries}
	h.R.Page(w, status, "admin/shop_form", p)
}

func (h *Handler) shopNew(w http.ResponseWriter, r *http.Request) {
	h.renderShopForm(w, r, http.StatusOK, shopForm{Country: h.countryParam(r), Active: true}, nil)
}

func (h *Handler) shopEdit(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sh, err := h.App.Shops.Get(r.Context(), id)
	if errors.Is(err, shops.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShopForm(w, r, http.StatusOK, shopForm{ID: sh.ID, Country: sh.CountryCode, Slug: sh.Slug, Name: sh.Name,
		Website: sh.Website, SearchURL: sh.SearchURLTemplate, Active: sh.Active}, nil)
}

func parseShopForm(r *http.Request) (shopForm, *shops.Shop) {
	f := shopForm{Country: r.FormValue("country"), Slug: r.FormValue("slug"), Name: r.FormValue("name"),
		Website: r.FormValue("website"), SearchURL: r.FormValue("search_url_template"), Active: r.FormValue("active") == "1"}
	sh := &shops.Shop{CountryCode: f.Country, Slug: strings.TrimSpace(f.Slug), Name: f.Name, Website: f.Website,
		SearchURLTemplate: strings.TrimSpace(f.SearchURL), Active: f.Active}
	return f, sh
}

func (h *Handler) saveShop(w http.ResponseWriter, r *http.Request, id int64) {
	f, sh := parseShopForm(r)
	f.ID = id
	sh.ID = id
	var err error
	if id == 0 {
		err = h.App.Shops.Create(r.Context(), sh)
	} else {
		err = h.App.Shops.Update(r.Context(), sh)
	}
	var ve shops.ValidationError
	switch {
	case err == nil:
		web.Redirect(w, r, "/admin/shops", "saved")
		return
	case errors.Is(err, shops.ErrNotFound):
		http.NotFound(w, r)
		return
	case errors.As(err, &ve):
		h.renderShopForm(w, r, http.StatusUnprocessableEntity, f, ve)
	default:
		h.fail(w, r, err)
		return
	}
}

func (h *Handler) shopCreate(w http.ResponseWriter, r *http.Request) { h.saveShop(w, r, 0) }

func (h *Handler) shopUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h.saveShop(w, r, id)
}

func (h *Handler) shopDelete(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Products of the shop stay; they fall back to the country's generic
	// catalog (products.shop_id becomes NULL).
	if err := h.App.Shops.Delete(r.Context(), id); errors.Is(err, shops.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		h.fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/admin/shops", "deleted")
}

// shopOptions lists a country's active shops for the product form.
func (h *Handler) shopOptions(r *http.Request, country string) []*shops.Shop {
	if country == "" {
		return nil
	}
	list, err := h.App.Shops.List(r.Context(), country, true)
	if err != nil {
		h.Log.Error("shop options", "err", err)
		return nil
	}
	return list
}
