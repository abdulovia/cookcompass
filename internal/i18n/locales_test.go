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

// TestShippedLocales checks the real translation files: Russian is complete,
// partial locales only use known ids, and placeholders always match English.
func TestShippedLocales(t *testing.T) {
	en := readLocale(t, "en")
	files, _ := filepath.Glob(filepath.Join(localesDir, "*.json"))
	if len(files) < 5 {
		t.Fatalf("expected en, ru and at least three partial locales, got %v", files)
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
		case "en", "ru":
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
	if got := b.Localizer("de").T("settings.prefs_title"); got != "Allergies & dislikes" {
		t.Errorf("partial locale should fall back to English: %q", got)
	}
}

func isMap(v any) bool { _, ok := v.(map[string]any); return ok }
