package site

import (
	"net/http"
	"strconv"

	"github.com/abdulovia/cookcompass/internal/web"
)

// maxGeoKm is how far from a known city a position may be and still be
// matched; farther away we keep the current country instead of guessing.
const maxGeoKm = 1500

// locate reverse-geocodes browser coordinates against the bundled city
// list (no external service) and stores the country and city.
func (h *Handler) locate(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	lat, errLat := strconv.ParseFloat(r.FormValue("lat"), 64)
	lng, errLng := strconv.ParseFloat(r.FormValue("lng"), 64)
	if errLat != nil || errLng != nil {
		web.Redirect(w, r, "/settings", "location_failed")
		return
	}
	city, km, err := h.App.Catalog.NearestCity(r.Context(), lat, lng)
	if err != nil || km > maxGeoKm {
		web.Redirect(w, r, "/settings", "location_failed")
		return
	}
	st := prof.Settings
	st.CountryCode, st.CityID = city.CountryCode, city.ID
	if err := h.App.Users.SaveSettings(r.Context(), prof.ID, st); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/settings", "location_detected")
}

// rescan refreshes the product catalog for the user's country. New shop
// items still need an admin's approval before they change availability.
func (h *Handler) rescan(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	if _, err := h.App.ScanCatalog(r.Context(), prof.CountryCode); err != nil {
		h.Log.Warn("rescan", "err", err)
		web.Redirect(w, r, "/settings", "scrape_failed")
		return
	}
	web.Redirect(w, r, "/settings", "scanned")
}
