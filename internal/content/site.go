package content

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Text is a display string that may vary by locale. In YAML it is either a
// plain string or a map of locale → string.
type Text map[string]string

// UnmarshalYAML accepts "Name" as well as {en: Name, ru: Имя}.
func (t *Text) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*t = Text{"": n.Value}
		return nil
	}
	m := map[string]string{}
	if err := n.Decode(&m); err != nil {
		return err
	}
	*t = m
	return nil
}

// In returns the text for locale, falling back to English, then to the
// untagged value; "" when nothing is set.
func (t Text) In(locale string) string {
	for _, k := range []string{locale, "en", ""} {
		if v := strings.TrimSpace(t[k]); v != "" {
			return v
		}
	}
	return ""
}

// Link is a footer link.
type Link struct {
	Label Text   `yaml:"label"`
	URL   string `yaml:"url"`
}

// Features switch whole areas of the public site on or off. A disabled
// feature has no routes, no navigation entry and no buttons.
const (
	FeatureShoppingList = "shopping_list"
	FeatureLearning     = "learning"
	FeatureGoals        = "goals"
	FeatureFavorites    = "favorites"
	FeatureGeolocation  = "geolocation"
)

// Site is the operator-facing presentation config (site.yaml).
type Site struct {
	// Name replaces the app name everywhere when set ("" = UI text app.name).
	Name Text `yaml:"name"`
	// Logo is an emoji/short text, or a /static/... image path.
	Logo    string `yaml:"logo"`
	Tagline Text   `yaml:"tagline"`
	// Accent is the brand colour (#rgb / #rrggbb); "" keeps the bundled one.
	Accent string `yaml:"accent"`
	// Stylesheet is an extra CSS file loaded after the bundled one, e.g.
	// /static/custom.css placed in content/static/.
	Stylesheet  string          `yaml:"stylesheet"`
	FooterLinks []Link          `yaml:"footer_links"`
	Features    map[string]bool `yaml:"features"`
	// DocsURL is where the admin checklist links its guides (install.md,
	// recipes.md, content.md are appended).
	DocsURL string `yaml:"docs_url"`
}

// DefaultDocsURL points at the project's guides on GitHub.
const DefaultDocsURL = "https://github.com/abdulovia/cookcompass/blob/main/docs/"

// Docs returns the guide URL for a docs file name, e.g. "recipes.md".
func (s *Site) Docs(file string) string {
	base := DefaultDocsURL
	if s != nil && s.DocsURL != "" {
		base = s.DocsURL
	}
	return strings.TrimRight(base, "/") + "/" + file
}

// DefaultSite is used when no site.yaml exists anywhere.
func DefaultSite() *Site {
	return &Site{Logo: "🧭", Features: map[string]bool{
		FeatureShoppingList: true, FeatureLearning: true, FeatureGoals: true, FeatureFavorites: true, FeatureGeolocation: true,
	}}
}

// On reports whether a feature is enabled. Unknown features are on, so a
// typo in site.yaml never hides a page silently — Validate reports it.
func (s *Site) On(feature string) bool {
	if s == nil || s.Features == nil {
		return true
	}
	v, ok := s.Features[feature]
	return !ok || v
}

var reColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// Validate rejects values that would break pages or inject markup.
func (s *Site) Validate() error {
	if s.Accent != "" && !reColor.MatchString(s.Accent) {
		return fmt.Errorf("site.yaml: accent %q must be #rgb or #rrggbb", s.Accent)
	}
	for _, p := range []string{s.Stylesheet, logoPath(s.Logo)} {
		if p != "" && (!strings.HasPrefix(p, "/static/") || strings.Contains(p, "..")) {
			return fmt.Errorf("site.yaml: %q must be a /static/... path", p)
		}
	}
	known := DefaultSite().Features
	for f := range s.Features {
		if _, ok := known[f]; !ok {
			return fmt.Errorf("site.yaml: unknown feature %q", f)
		}
	}
	if s.DocsURL != "" && !strings.HasPrefix(s.DocsURL, "https://") && !strings.HasPrefix(s.DocsURL, "http://") {
		return fmt.Errorf("site.yaml: docs_url %q must be http(s)://", s.DocsURL)
	}
	for _, l := range s.FooterLinks {
		if !strings.HasPrefix(l.URL, "https://") && !strings.HasPrefix(l.URL, "http://") && !strings.HasPrefix(l.URL, "/") {
			return fmt.Errorf("site.yaml: footer link %q must be http(s):// or a /path", l.URL)
		}
	}
	return nil
}

// LogoImage reports whether Logo is an image path rather than text.
func (s *Site) LogoImage() bool { return logoPath(s.Logo) != "" }

func logoPath(logo string) string {
	if strings.HasPrefix(logo, "/") {
		return logo
	}
	return ""
}

// LoadSite reads site.yaml from every layer, each one refining the
// previous (bundled defaults first, then the content directory).
func LoadSite(fsys fs.FS) (*Site, error) {
	s := DefaultSite()
	layers, err := ReadLayers(fsys, "site.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	for _, data := range layers {
		if err := yaml.Unmarshal(data, s); err != nil {
			return nil, fmt.Errorf("site.yaml: %w", err)
		}
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}
