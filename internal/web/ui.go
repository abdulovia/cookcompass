package web

import (
	"github.com/abdulovia/cookcompass/internal/content"
	"github.com/abdulovia/cookcompass/internal/nutrition"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Enum is a closed set of domain values shown in the UI. Values come from
// the domain packages; labels are UI text "<Prefix><value>" (with an
// optional "<Prefix><value>_desc" description).
type Enum struct {
	Prefix string
	Values []string
}

// Enums is the one table templates use for option lists and value labels,
// instead of spelling out "printf \"meal.%s\"" everywhere.
var Enums = map[string]Enum{
	"meal":       {"meal.", recipes.MealTimes},
	"difficulty": {"difficulty.", recipes.Difficulties},
	"activity":   {"activity.", nutrition.Activities},
	"goal":       {"goal.", nutrition.Goals},
	"sex":        {"sex.", []string{nutrition.Female, nutrition.Male}},
	"units":      {"settings.units_", []string{recipes.Metric, recipes.Imperial}},
	"status":     {"status.", []string{recipes.StatusApproved, recipes.StatusPending, recipes.StatusRejected}},
	"nutrient":   {"nutrition.", []string{"calories", "protein", "fat", "carbs", "fiber", "sugar", "sodium", "saturated_fat"}},
}

// Option is one entry of an option list.
type Option struct {
	Value    string
	Label    string
	Desc     string
	Selected bool
}

// E returns the label of one enum value, e.g. {{.E "difficulty" .Difficulty}}.
func (p *Page) E(kind, value string) string {
	if e, ok := Enums[kind]; ok {
		return p.T(e.Prefix + value)
	}
	return value
}

// Options lists an enum with labels and the current selection; selected is
// a string or a []string (multi-select).
//
//	{{range .Options "meal" .Data.Q.MealTime}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Label}}</option>{{end}}
func (p *Page) Options(kind string, selected any) []Option {
	e, ok := Enums[kind]
	if !ok {
		return nil
	}
	sel := map[string]bool{}
	switch v := selected.(type) {
	case string:
		sel[v] = true
	case []string:
		for _, s := range v {
			sel[s] = true
		}
	}
	out := make([]Option, len(e.Values))
	for i, v := range e.Values {
		o := Option{Value: v, Label: p.T(e.Prefix + v), Selected: sel[v]}
		if d := p.T(e.Prefix + v + "_desc"); d != e.Prefix+v+"_desc" {
			o.Desc = d
		}
		out[i] = o
	}
	return out
}

// NavItem is one navigation entry; Feature "" means always shown.
type NavItem struct {
	Path    string
	Label   string // UI text id
	Feature string
	Exact   bool // active only on the exact path
}

// PublicNav and AdminNav are the single source of navigation. Routes for a
// disabled feature are not mounted either (see site.Handler.Routes).
var (
	PublicNav = []NavItem{
		{Path: "/", Label: "nav.home", Exact: true},
		{Path: "/recipes", Label: "nav.recipes"},
		{Path: "/learn", Label: "nav.learn", Feature: content.FeatureLearning},
		{Path: "/list", Label: "nav.list", Feature: content.FeatureShoppingList},
		{Path: "/goals", Label: "nav.goals", Feature: content.FeatureGoals},
		{Path: "/settings", Label: "nav.settings"},
	}
	AdminNav = []NavItem{
		{Path: "/admin", Label: "admin.title", Exact: true},
		{Path: "/admin/recipes", Label: "admin.recipes"},
		{Path: "/admin/products", Label: "admin.products"},
		{Path: "/admin/shops", Label: "admin.shops"},
		{Path: "/admin/queue", Label: "admin.queue"},
	}
)

// NavLink is a rendered navigation entry.
type NavLink struct {
	Path   string
	Label  string
	Active bool
}

// Nav returns the navigation for the current area with disabled features
// removed and the current entry marked.
func (p *Page) Nav() []NavLink {
	items := PublicNav
	if p.Admin {
		items = AdminNav
	}
	out := make([]NavLink, 0, len(items))
	for _, it := range items {
		if it.Feature != "" && !p.Feature(it.Feature) {
			continue
		}
		active := p.Path == it.Path
		if !it.Exact && !active {
			active = p.Active(it.Path)
		}
		out = append(out, NavLink{Path: it.Path, Label: p.T(it.Label), Active: active})
	}
	return out
}

// Site returns the site presentation config (never nil).
func (p *Page) Site() *content.Site {
	if p.ui != nil && p.ui.Site != nil {
		return p.ui.Site
	}
	return content.DefaultSite()
}

// Feature reports whether a site feature is enabled.
func (p *Page) Feature(name string) bool { return p.Site().On(name) }

// SiteName is the configured site name in the page language, else the UI
// text "app.name".
func (p *Page) SiteName() string {
	if n := p.Site().Name.In(p.Lang); n != "" {
		return n
	}
	return p.T("app.name")
}

// Tagline is the configured tagline in the page language ("" if unset).
func (p *Page) Tagline() string { return p.Site().Tagline.In(p.Lang) }

// FooterLink is a rendered footer link.
type FooterLink struct{ Label, URL string }

// FooterLinks returns the configured footer links in the page language.
func (p *Page) FooterLinks() []FooterLink {
	links := p.Site().FooterLinks
	out := make([]FooterLink, 0, len(links))
	for _, l := range links {
		out = append(out, FooterLink{Label: l.Label.In(p.Lang), URL: l.URL})
	}
	return out
}
