package site

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/users"
	"github.com/abdulovia/cookcompass/internal/web"
)

// LangCookie mirrors server.LangCookie (kept here to avoid an import cycle).
const LangCookie = "lang"

// IngredientGroup is a category of ingredients for checkbox grids.
type IngredientGroup struct {
	Category string
	Items    []ingredients.Ingredient
}

func groupIngredients(list []ingredients.Ingredient, skipStaples bool) []IngredientGroup {
	idx := map[string]int{}
	var out []IngredientGroup
	for _, in := range list {
		if skipStaples && in.Staple {
			continue
		}
		i, ok := idx[in.Category]
		if !ok {
			i = len(out)
			idx[in.Category] = i
			out = append(out, IngredientGroup{Category: in.Category})
		}
		out[i].Items = append(out[i].Items, in)
	}
	return out
}

// requireUser returns the profile if it can be modified; in multi-user mode
// guests are sent to the login page.
func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) *users.Profile {
	p := web.Profile(r.Context())
	if p == nil || p.ID == 0 {
		web.Redirect(w, r, "/login", "login_required")
		return nil
	}
	return p
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	prof := web.Profile(ctx)
	countries, err := h.App.Catalog.Countries(ctx)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	country := ""
	if prof != nil {
		country = prof.CountryCode
	}
	places, err := h.placeData(r, prof, country)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	list, err := h.App.Ings.List(ctx)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "settings.title")
	lang := p.Lang
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Category != list[j].Category {
			return list[i].Category < list[j].Category
		}
		return list[i].LocalName(lang) < list[j].LocalName(lang)
	})
	data := map[string]any{"Countries": countries, "Groups": groupIngredients(list, true), "CountryAware": h.CountryAware}
	for k, v := range places {
		data[k] = v
	}
	p.Data = data
	h.R.Page(w, http.StatusOK, "pages/settings", p)
}

// placeData lists the cities and shops of country; the saved city and shop
// stay selected only while the country is the saved one.
func (h *Handler) placeData(r *http.Request, prof *users.Profile, country string) (map[string]any, error) {
	ctx := r.Context()
	data := map[string]any{"Cities": []catalog.City(nil), "Shops": []*shops.Shop(nil), "CityID": int64(0), "ShopID": int64(0)}
	if country == "" {
		return data, nil
	}
	cities, err := h.App.Catalog.Cities(ctx, country)
	if err != nil {
		return nil, err
	}
	shopList, err := h.App.Shops.List(ctx, country, true)
	if err != nil {
		return nil, err
	}
	data["Cities"], data["Shops"] = cities, shopList
	if prof != nil && prof.CountryCode == country {
		data["CityID"], data["ShopID"] = prof.CityID, prof.ShopID
	}
	return data, nil
}

// places renders the city and shop pickers for the country chosen in the
// form, so switching country updates them before saving (HTMX swap).
func (h *Handler) places(w http.ResponseWriter, r *http.Request) {
	country := r.URL.Query().Get("country")
	if _, err := h.App.Catalog.Country(r.Context(), country); err != nil {
		country = ""
	}
	data, err := h.placeData(r, web.Profile(r.Context()), country)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	p := web.NewPage(r, "")
	p.Data = data
	h.R.Partial(w, http.StatusOK, "pages/settings", "place_fields", p)
}

func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	ctx := r.Context()
	st := prof.Settings
	if c := r.FormValue("country"); c != "" {
		if _, err := h.App.Catalog.Country(ctx, c); err == nil {
			if c != st.CountryCode {
				st.CityID = 0
			}
			st.CountryCode = c
		}
	}
	if id, err := strconv.ParseInt(r.FormValue("city"), 10, 64); err == nil {
		if city, err := h.App.Catalog.City(ctx, id); err == nil && city.CountryCode == st.CountryCode {
			st.CityID = id
		}
	} else if r.FormValue("city") == "" {
		st.CityID = 0
	}
	st.Units = oneOf(r.FormValue("units"), []string{recipes.Metric, recipes.Imperial})
	lang := r.FormValue("lang")
	if lang == "" || h.Bundle.Has(lang) {
		st.Locale = lang
		if lang == "" {
			http.SetCookie(w, &http.Cookie{Name: LangCookie, Value: "", Path: "/", MaxAge: -1})
		} else {
			http.SetCookie(w, &http.Cookie{Name: LangCookie, Value: lang, Path: "/", MaxAge: 365 * 24 * 3600,
				HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.App.Cfg.Auth.SecureCookies})
		}
	}
	st.ShowUnavailable = r.FormValue("show_unavailable") == "1"
	// The shop must belong to the (possibly just changed) country; anything
	// else clears the selection.
	st.ShopID = 0
	if id, err := strconv.ParseInt(r.FormValue("shop"), 10, 64); err == nil && id > 0 {
		if sh, err := h.App.Shops.Get(ctx, id); err == nil && sh.CountryCode == st.CountryCode && sh.Active {
			st.ShopID = id
		}
	}
	st.StrictShop = r.FormValue("strict_shop") == "1"
	if err := h.App.Users.SaveSettings(ctx, prof.ID, st); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/settings", "settings_saved")
}

func parseIDs(values []string) []int64 {
	var out []int64
	for _, v := range values {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func (h *Handler) savePreferences(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.Fail(w, r, err)
		return
	}
	prefs := map[int64]string{}
	for _, id := range parseIDs(r.PostForm["dislike"]) {
		prefs[id] = users.Dislike
	}
	// An allergy outranks a dislike for the same ingredient.
	for _, id := range parseIDs(r.PostForm["allergy"]) {
		prefs[id] = users.Allergy
	}
	if err := h.App.Users.SetPreferences(r.Context(), prof.ID, prefs); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/settings#preferences", "settings_saved")
}

func (h *Handler) savePantry(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.Fail(w, r, err)
		return
	}
	if err := h.App.Users.SetPantry(r.Context(), prof.ID, parseIDs(r.PostForm["pantry"])); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/settings#pantry", "settings_saved")
}
