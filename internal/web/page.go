package web

import (
	"context"
	"net/http"
	"net/url"

	"github.com/abdulovia/cookcompass/internal/i18n"
	"github.com/abdulovia/cookcompass/internal/users"
)

type ctxKey int

const (
	localizerKey ctxKey = iota
	langsKey
	profileKey
	multiUserKey
)

// WithProfile stores the current user's profile in ctx.
func WithProfile(ctx context.Context, p *users.Profile, multiUser bool) context.Context {
	ctx = context.WithValue(ctx, profileKey, p)
	return context.WithValue(ctx, multiUserKey, multiUser)
}

// Profile returns the current user's profile (never nil inside the app's
// middleware chain).
func Profile(ctx context.Context) *users.Profile {
	p, _ := ctx.Value(profileKey).(*users.Profile)
	return p
}

// Page is the data passed to every template.
type Page struct {
	Title     string
	Lang      string
	Languages []i18n.Language
	Path      string
	Query     url.Values
	Flash     string
	Errors    map[string]string
	Admin     bool
	User      *users.Profile
	// MultiUser shows login/logout links.
	MultiUser bool
	Data      any

	loc *i18n.Localizer
}

// T translates id with optional key/value template data.
func (p *Page) T(id string, kv ...any) string {
	if p.loc == nil {
		return id
	}
	return p.loc.T(id, kv...)
}

// Err returns the translated validation message for a form field.
func (p *Page) Err(field string) string {
	if m, ok := p.Errors[field]; ok {
		return p.T(m)
	}
	return ""
}

// HasErrors reports whether any validation errors are present.
func (p *Page) HasErrors() bool { return len(p.Errors) > 0 }

// Active reports whether the current path starts with prefix (nav highlighting).
func (p *Page) Active(prefix string) bool {
	if prefix == "/" {
		return p.Path == "/"
	}
	return len(p.Path) >= len(prefix) && p.Path[:len(prefix)] == prefix
}

// WithLocalizer stores the request localizer and base page in ctx.
func WithLocalizer(ctx context.Context, loc *i18n.Localizer, langs []i18n.Language) context.Context {
	ctx = context.WithValue(ctx, localizerKey, loc)
	return context.WithValue(ctx, langsKey, langs)
}

// Localizer returns the request localizer (English if none).
func Localizer(ctx context.Context) *i18n.Localizer {
	if l, ok := ctx.Value(localizerKey).(*i18n.Localizer); ok {
		return l
	}
	return nil
}

// NewPage builds the base page data for a request. Flash messages travel as
// a "flash" query parameter holding a message id (no server-side state).
func NewPage(r *http.Request, title string) *Page {
	p := &Page{Path: r.URL.Path, Query: r.URL.Query(), loc: Localizer(r.Context())}
	if p.loc != nil {
		p.Lang = p.loc.Lang
	} else {
		p.Lang = i18n.Default
	}
	if langs, ok := r.Context().Value(langsKey).([]i18n.Language); ok {
		p.Languages = langs
	}
	p.User = Profile(r.Context())
	p.MultiUser, _ = r.Context().Value(multiUserKey).(bool)
	if f := r.URL.Query().Get("flash"); isFlashID(f) {
		p.Flash = "flash." + f
	}
	if title != "" {
		p.Title = p.T(title)
	}
	return p
}

func isFlashID(s string) bool {
	if s == "" || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c == '_') {
			return false
		}
	}
	return true
}

// Redirect sends a 303 to target with an optional flash message id.
func Redirect(w http.ResponseWriter, r *http.Request, target, flash string) {
	if flash != "" {
		u, err := url.Parse(target)
		if err == nil {
			q := u.Query()
			q.Set("flash", flash)
			u.RawQuery = q.Encode()
			target = u.String()
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
