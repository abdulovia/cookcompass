package i18n

import (
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"loc/en.json": {Data: []byte(`{"meta.language_name":"English","hello":"Hello, {{.Name}}!","only.en":"English only",
			"items":{"one":"{{.Count}} item","other":"{{.Count}} items"}}`)},
		"loc/ru.json": {Data: []byte(`{"meta.language_name":"Русский","hello":"Привет, {{.Name}}!",
			"items":{"one":"{{.Count}} штука","few":"{{.Count}} штуки","many":"{{.Count}} штук","other":"{{.Count}} штуки"}}`)},
		"loc/de.json": {Data: []byte(`{"meta.language_name":"Deutsch"}`)},
	}
}

func TestLoadAndTranslate(t *testing.T) {
	b, err := Load(testFS(), "loc")
	if err != nil {
		t.Fatal(err)
	}
	langs := b.Languages()
	if len(langs) != 3 || langs[0].Code != "en" || langs[1].Code != "de" || langs[2].Name != "Русский" {
		t.Fatalf("languages %+v", langs)
	}
	if !langs[0].Complete() || langs[1].Complete() || langs[2].Complete() {
		t.Fatalf("coverage flags wrong: %+v", langs)
	}
	ru := b.Localizer("ru")
	tests := []struct {
		l    *Localizer
		id   string
		args []any
		want string
	}{
		{ru, "hello", []any{"Name", "Аня"}, "Привет, Аня!"},
		{ru, "only.en", nil, "English only"},        // falls back to English
		{ru, "missing.key", nil, "missing.key"},     // unknown ids are visible
		{ru, "items", []any{"Count", 1}, "1 штука"}, // Russian plural: one
		{ru, "items", []any{"Count", 3}, "3 штуки"}, // few
		{ru, "items", []any{"Count", 5}, "5 штук"},  // many
		{b.Localizer("en"), "items", []any{"Count", 2}, "2 items"},
		{b.Localizer("de"), "hello", []any{"Name", "Max"}, "Hello, Max!"},
		{ru, "only.en", []any{"Count", 2}, "English only"}, // Count on a non-plural message
	}
	for _, tc := range tests {
		if got := tc.l.T(tc.id, tc.args...); got != tc.want {
			t.Errorf("%s T(%q) = %q want %q", tc.l.Lang, tc.id, got, tc.want)
		}
	}
	if !b.Has("ru") || b.Has("fr") {
		t.Fatal("Has wrong")
	}
}

func TestMatch(t *testing.T) {
	b, err := Load(testFS(), "loc")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ pref, accept, want string }{
		{"", "", "en"},
		{"", "ru-RU,ru;q=0.9,en;q=0.8", "ru"},
		{"", "de-AT", "de"},
		{"", "fr-FR,fr;q=0.9", "en"},
		{"en", "ru", "en"},
		{"xx", "ru", "ru"},
		{"", "garbage;;q=", "en"},
	}
	for _, tc := range tests {
		if got := b.Match(tc.pref, tc.accept); got != tc.want {
			t.Errorf("Match(%q, %q) = %q want %q", tc.pref, tc.accept, got, tc.want)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"no english":  {"loc/ru.json": {Data: []byte(`{"a":"b"}`)}},
		"bad json":    {"loc/en.json": {Data: []byte(`{`)}},
		"bad code":    {"loc/en.json": {Data: []byte(`{}`)}, "loc/not a lang.json": {Data: []byte(`{}`)}},
		"bad pattern": {"loc/en.json": {Data: []byte(`{}`)}},
	}
	for name, fsys := range cases {
		dir := "loc"
		if name == "bad pattern" {
			dir = "["
		}
		if _, err := Load(fsys, dir); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	b, err := Load(fstest.MapFS{"loc/en.json": {Data: []byte(`{}`)}}, "loc")
	if err != nil || b.Languages()[0].Name != "en" {
		t.Fatalf("missing language name should fall back to code: %v", err)
	}
}
