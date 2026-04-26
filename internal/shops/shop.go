// Package shops manages the grocery shops a user can buy from: every shop
// carries a search URL template so any ingredient can get a "buy" link, and
// scanned or hand-entered products can carry exact product pages.
package shops

import (
	"net/url"
	"regexp"
	"strings"
)

// QueryPlaceholder marks where the search term goes in SearchURLTemplate.
const QueryPlaceholder = "{q}"

// Shop is a grocery shop or online store in one country.
type Shop struct {
	ID                int64
	CountryCode       string
	Slug              string
	Name              string
	Website           string
	SearchURLTemplate string // e.g. https://shop.example/search?q={q}
	Active            bool
}

// SearchURLFor builds the shop's search URL for query. It returns "" when the
// shop has no template.
func (s Shop) SearchURLFor(query string) string {
	if s.SearchURLTemplate == "" {
		return ""
	}
	return strings.ReplaceAll(s.SearchURLTemplate, QueryPlaceholder, url.QueryEscape(query))
}

// ValidationError maps field → i18n message id.
type ValidationError map[string]string

func (v ValidationError) Error() string {
	var parts []string
	for k, m := range v {
		parts = append(parts, k+": "+m)
	}
	return "invalid shop: " + strings.Join(parts, "; ")
}

var reSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// Slugify turns a shop name into a URL-safe slug. It returns "" when nothing
// latin remains; callers should then ask for an explicit slug.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = reSlug.ReplaceAllString(s, "")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// validate normalizes and checks a shop. It mirrors catalog validation:
// country must be a 2-letter code, the search template (when set) must be an
// http(s) URL containing {q}.
func validate(s *Shop) error {
	errs := ValidationError{}
	s.Name = strings.TrimSpace(s.Name)
	s.CountryCode = strings.ToUpper(strings.TrimSpace(s.CountryCode))
	s.Website = strings.TrimSpace(s.Website)
	s.SearchURLTemplate = strings.TrimSpace(s.SearchURLTemplate)
	if s.Name == "" {
		errs["name"] = "validation.required"
	}
	if len(s.CountryCode) != 2 {
		errs["country"] = "validation.invalid_choice"
	}
	if s.Slug == "" {
		s.Slug = Slugify(s.Name)
	}
	if s.Slug == "" {
		errs["slug"] = "validation.required"
	}
	for field, u := range map[string]string{"website": s.Website, "search_url_template": s.SearchURLTemplate} {
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			errs[field] = "validation.invalid_choice"
		}
	}
	if s.SearchURLTemplate != "" && !strings.Contains(s.SearchURLTemplate, QueryPlaceholder) {
		errs["search_url_template"] = "validation.shop_template_placeholder"
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}
