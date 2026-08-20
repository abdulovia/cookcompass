package content

import (
	"errors"
	"io/fs"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Label kinds: reference data whose display names live with the data
// (seed/*.json, content/*.json) instead of in the UI locale files.
const (
	Cuisine  = "cuisine"
	Country  = "country"
	Category = "category"
)

// named is the common shape of a labelled reference entry.
type named struct {
	Slug  string            `json:"slug"`
	Code  string            `json:"code"`
	Name  string            `json:"name"`
	Names map[string]string `json:"names"`
}

func (n named) key() string {
	if n.Code != "" {
		return strings.ToUpper(n.Code)
	}
	return n.Slug
}

// Labels resolves display names of cuisines, countries and categories.
type Labels struct {
	byKind map[string]map[string]map[string]string // kind → key → locale → name
}

var labelFiles = map[string]string{Cuisine: "cuisines.json", Country: "countries.json", Category: "categories.json"}

// LoadLabels reads the labelled reference files from a (layered) seed file
// system. Missing files are fine; broken ones are an error.
func LoadLabels(fsys fs.FS) (*Labels, error) {
	l := &Labels{byKind: map[string]map[string]map[string]string{}}
	for kind, file := range labelFiles {
		list, err := MergeJSON(fsys, file, named.key)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		m := map[string]map[string]string{}
		for _, n := range list {
			names := map[string]string{}
			if n.Name != "" {
				names["en"] = n.Name
			}
			for loc, v := range n.Names {
				if v = strings.TrimSpace(v); v != "" {
					names[loc] = v
				}
			}
			m[n.key()] = names
		}
		l.byKind[kind] = m
	}
	return l, nil
}

// Label returns the name of key for locale: the locale's name, else the
// English one, else a readable form of the key ("middle-eastern" →
// "Middle eastern"). ok reports whether the data knew the key.
func (l *Labels) Label(kind, key, locale string) (string, bool) {
	if kind == Country {
		key = strings.ToUpper(key)
	}
	if l != nil {
		if names, found := l.byKind[kind][key]; found {
			if v := names[locale]; v != "" {
				return v, true
			}
			if v := names["en"]; v != "" {
				return v, true
			}
		}
	}
	return Humanize(key), false
}

// Humanize turns a slug into a sentence-case label.
func Humanize(slug string) string {
	s := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(slug))
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
