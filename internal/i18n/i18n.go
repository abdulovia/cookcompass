// Package i18n loads UI translations from JSON files and picks a language
// for each request. English is the fallback for anything missing.
package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Default is the fallback language.
const Default = "en"

// Language describes an available UI language.
type Language struct {
	Code     string  // BCP 47, e.g. "ru"
	Name     string  // native name, from the "meta.language_name" message
	Coverage float64 // share of English message ids translated, 0..1
}

// Complete reports whether every English message is translated.
func (l Language) Complete() bool { return l.Coverage >= 0.999 }

// Bundle holds all loaded translations.
type Bundle struct {
	bundle  *goi18n.Bundle
	langs   []Language
	byCode  map[string]Language
	matcher language.Matcher
}

// Load reads every <code>.json file in dir of fsys.
func Load(fsys fs.FS, dir string) (*Bundle, error) {
	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	files, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	ids := map[string]map[string]bool{}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		code := strings.TrimSuffix(path.Base(f), ".json")
		if _, err := language.Parse(code); err != nil {
			return nil, fmt.Errorf("%s: bad language code: %w", f, err)
		}
		mf, err := b.ParseMessageFileBytes(data, f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		set := map[string]bool{}
		for _, m := range mf.Messages {
			set[m.ID] = true
		}
		ids[code] = set
	}
	en, ok := ids[Default]
	if !ok {
		return nil, fmt.Errorf("i18n: %s/%s.json is required", dir, Default)
	}
	bd := &Bundle{bundle: b, byCode: map[string]Language{}}
	codes := make([]string, 0, len(ids))
	for c := range ids {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool {
		if codes[i] == Default || codes[j] == Default {
			return codes[i] == Default
		}
		return codes[i] < codes[j]
	})
	tags := make([]language.Tag, 0, len(codes))
	for _, c := range codes {
		n := 0
		for id := range en {
			if ids[c][id] {
				n++
			}
		}
		lang := Language{Code: c, Coverage: float64(n) / float64(max(len(en), 1))}
		lang.Name = bd.Localizer(c).T("meta.language_name")
		if lang.Name == "meta.language_name" {
			lang.Name = c
		}
		bd.langs = append(bd.langs, lang)
		bd.byCode[c] = lang
		tags = append(tags, language.MustParse(c))
	}
	bd.matcher = language.NewMatcher(tags)
	return bd, nil
}

// Languages lists available languages, English first.
func (b *Bundle) Languages() []Language { return b.langs }

// Has reports whether code is an available language.
func (b *Bundle) Has(code string) bool {
	_, ok := b.byCode[code]
	return ok
}

// Match picks the best available language: an explicit preference (cookie
// or user setting) wins, then the Accept-Language header, then English.
func (b *Bundle) Match(preferred, acceptLanguage string) string {
	if preferred != "" && b.Has(preferred) {
		return preferred
	}
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil || len(tags) == 0 {
		return Default
	}
	_, idx, conf := b.matcher.Match(tags...)
	if conf == language.No {
		return Default
	}
	return b.langs[idx].Code
}

// Localizer translates messages for one language.
type Localizer struct {
	Lang string
	l    *goi18n.Localizer
}

// Localizer returns a translator for lang with English fallback.
func (b *Bundle) Localizer(lang string) *Localizer {
	return &Localizer{Lang: lang, l: goi18n.NewLocalizer(b.bundle, lang, Default)}
}

// T translates id. Arguments are key/value pairs used as template data; a
// "Count" key selects the plural form. Unknown ids return the id itself so
// gaps are visible rather than fatal.
func (l *Localizer) T(id string, kv ...any) string {
	cfg := &goi18n.LocalizeConfig{MessageID: id}
	if len(kv) > 0 {
		data := make(map[string]any, len(kv)/2)
		for i := 0; i+1 < len(kv); i += 2 {
			k := fmt.Sprint(kv[i])
			data[k] = kv[i+1]
			if k == "Count" {
				cfg.PluralCount = kv[i+1]
			}
		}
		cfg.TemplateData = data
	}
	// When the message only exists in English, go-i18n returns the English
	// text together with a MessageNotFoundErr; the text is what we want.
	s, _ := l.l.Localize(cfg)
	if s == "" {
		return id
	}
	return s
}
