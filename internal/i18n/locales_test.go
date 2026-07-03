package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const localesDir = "../../web/locales"

var rePlaceholder = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

// reBareBrace catches ICU-style "{Count}" placeholders, which go-i18n prints
// literally (it wants "{{.Count}}"). Lowercase "{q}" is shop-URL syntax.
var reBareBrace = regexp.MustCompile(`(^|[^{])\{[A-Z]\w*\}`)

func strs(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case map[string]any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func readLocale(t *testing.T, code string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(localesDir, code+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s.json: %v", code, err)
	}
	return m
}

func placeholders(v any) string {
	set := map[string]bool{}
	add := func(s string) {
		for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
			set[m[1]] = true
		}
	}
	switch t := v.(type) {
	case string:
		add(t)
	case map[string]any:
		for _, x := range t {
			if s, ok := x.(string); ok {
				add(s)
			}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// TestShippedLocales checks the real translation files: en, ru, de, es and fr
// are complete, any other locale only uses known ids, and placeholders always
// match English.
func TestShippedLocales(t *testing.T) {
	en := readLocale(t, "en")
	files, _ := filepath.Glob(filepath.Join(localesDir, "*.json"))
	if len(files) < 5 {
		t.Fatalf("expected the five shipped locales, got %v", files)
	}
	for _, f := range files {
		code := strings.TrimSuffix(filepath.Base(f), ".json")
		loc := readLocale(t, code)
		for k, v := range loc {
			ev, ok := en[k]
			if !ok {
				t.Errorf("%s: unknown message id %q", code, k)
				continue
			}
			for _, str := range strs(v) {
				if reBareBrace.MatchString(str) {
					t.Errorf("%s %s: bare {Name} placeholder in %q; use {{.Name}}", code, k, str)
				}
			}
			if placeholders(v) != placeholders(ev) {
				t.Errorf("%s %s: placeholders %q, English has %q", code, k, placeholders(v), placeholders(ev))
			}
			if _, plural := ev.(map[string]any); plural != isMap(v) {
				t.Errorf("%s %s: plural shape differs from English", code, k)
			}
			if code == "ru" {
				if m, ok := v.(map[string]any); ok {
					for _, form := range []string{"one", "few", "many", "other"} {
						if _, ok := m[form]; !ok {
							t.Errorf("ru %s: missing plural form %q", k, form)
						}
					}
				}
			}
		}
		if loc["meta.language_name"] == nil {
			t.Errorf("%s: meta.language_name missing", code)
		}
	}
	ru := readLocale(t, "ru")
	for k := range en {
		if _, ok := ru[k]; !ok {
			t.Errorf("ru: missing %q", k)
		}
	}

	b, err := Load(os.DirFS("../../web"), "locales")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range b.Languages() {
		switch l.Code {
		case "en", "ru", "de", "es", "fr":
			if !l.Complete() {
				t.Errorf("%s should be complete (%.0f%%)", l.Code, l.Coverage*100)
			}
		default:
			if l.Complete() {
				t.Errorf("%s is marked partial in docs but is complete", l.Code)
			}
		}
	}
	if got := b.Localizer("ru").T("browse.count", "Count", 22); got != "22 рецепта" {
		t.Errorf("ru plural few: %q", got)
	}
	if got := b.Localizer("de").T("settings.prefs_title"); got != "Allergien & Abneigungen" {
		t.Errorf("German settings.prefs_title: %q", got)
	}
}

func isMap(v any) bool { _, ok := v.(map[string]any); return ok }

// TestFloatPluralCounts: quantities are float64; go-i18n refuses unformatted
// floats, which used to fall back to the "other" form for every unit.
func TestFloatPluralCounts(t *testing.T) {
	b, err := Load(os.DirFS("../../web"), "locales")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		lang  string
		count any
		want  string
	}{
		{"en", 1.0, "clove"},
		{"en", 2.0, "cloves"},
		{"en", 1.5, "cloves"},
		{"en", float32(1), "clove"},
		{"en", 1, "clove"},
		{"ru", 1.0, "зубчик"},
		{"ru", 3.0, "зубчика"},
		{"ru", 5.0, "зубчиков"},
		{"ru", 21.0, "зубчик"},
		{"ru", 1.5, "зубчика"},
	}
	for _, c := range cases {
		if got := b.Localizer(c.lang).T("unit.clove", "Count", c.count); got != c.want {
			t.Errorf("%s %v: got %q, want %q", c.lang, c.count, got, c.want)
		}
	}
	if got := b.Localizer("en").T("list.open_count", "Count", 3); got != "3 items to buy" {
		t.Errorf("list.open_count: %q", got)
	}
}
