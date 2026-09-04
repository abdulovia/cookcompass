package content

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var base = fstest.MapFS{
	"cuisines.json":         {Data: []byte(`[{"slug":"thai","name":"Thai"},{"slug":"greek","name":"Greek"}]`)},
	"recipes/pad-thai.json": {Data: []byte(`{"name":"Pad Thai"}`)},
	"locales/en.json":       {Data: []byte(`{"a":"A"}`)},
	"static/app.css":        {Data: []byte(`body{}`)},
}

func TestLayerMissingDirIsBase(t *testing.T) {
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "nope")} {
		if got := Layer(dir, base); got == nil {
			t.Fatal("nil fs")
		} else if _, layered := got.(*Layered); layered {
			t.Fatalf("%q: expected the base fs unchanged", dir)
		}
	}
}

func TestLayeredOverrideAndUnion(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"recipes/pad-thai.json": `{"name":"My Pad Thai"}`,
		"recipes/mine.json":     `{"name":"Mine"}`,
		"static/app.css":        `body{color:red}`,
		"static/logo.svg":       `<svg/>`,
	})
	fsys := Layer(dir, base)

	if b, _ := fs.ReadFile(fsys, "recipes/pad-thai.json"); !strings.Contains(string(b), "My Pad Thai") {
		t.Fatalf("top should win: %s", b)
	}
	if b, _ := fs.ReadFile(fsys, "cuisines.json"); !strings.Contains(string(b), "greek") {
		t.Fatal("base file should show through")
	}
	got, err := fs.Glob(fsys, "recipes/*.json")
	if err != nil || strings.Join(got, ",") != "recipes/mine.json,recipes/pad-thai.json" {
		t.Fatalf("glob union: %v %v", got, err)
	}
	entries, err := fs.ReadDir(fsys, "static")
	if err != nil || len(entries) != 2 {
		t.Fatalf("readdir union: %v %v", entries, err)
	}
	if _, err := fs.ReadFile(fsys, "missing.json"); err == nil {
		t.Fatal("missing file should error")
	}

	sub, err := Sub(fsys, "static")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := fs.ReadFile(sub, "app.css"); string(b) != "body{color:red}" {
		t.Fatalf("sub override: %s", b)
	}
	if _, err := fs.ReadFile(sub, "logo.svg"); err != nil {
		t.Fatalf("sub top-only file: %v", err)
	}

	layers, err := ReadLayers(fsys, "recipes/pad-thai.json")
	if err != nil || len(layers) != 2 || !strings.Contains(string(layers[1]), "My") {
		t.Fatalf("layers bottom-up: %q %v", layers, err)
	}
	if one, err := ReadLayers(base, "cuisines.json"); err != nil || len(one) != 1 {
		t.Fatalf("plain fs: %d %v", len(one), err)
	}
	if _, err := ReadLayers(fsys, "nope.json"); err == nil {
		t.Fatal("ReadLayers on a missing file should error")
	}
}

func TestMergeJSON(t *testing.T) {
	type cuisine struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	dir := writeFiles(t, map[string]string{
		"cuisines.json": `[{"slug":"greek","name":"Hellenic"},{"slug":"georgian","name":"Georgian"}]`,
	})
	list, err := MergeJSON(Layer(dir, base), "cuisines.json", func(c cuisine) string { return c.Slug })
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range list {
		got = append(got, c.Slug+"="+c.Name)
	}
	if want := "thai=Thai,greek=Hellenic,georgian=Georgian"; strings.Join(got, ",") != want {
		t.Fatalf("merge: %v, want %s", got, want)
	}

	bad := writeFiles(t, map[string]string{"cuisines.json": `{oops`})
	if _, err := MergeJSON(Layer(bad, base), "cuisines.json", func(c cuisine) string { return c.Slug }); err == nil ||
		!strings.Contains(err.Error(), "cuisines.json") {
		t.Fatalf("broken content file should name the file: %v", err)
	}
}

func TestLabels(t *testing.T) {
	seed := fstest.MapFS{
		"cuisines.json":  {Data: []byte(`[{"slug":"thai","name":"Thai","names":{"ru":"Тайская"}},{"slug":"greek","name":"Greek"}]`)},
		"countries.json": {Data: []byte(`[{"code":"US","name":"United States","names":{"ru":"США"}}]`)},
	}
	dir := writeFiles(t, map[string]string{
		"cuisines.json":   `[{"slug":"greek","name":"Hellenic","names":{"ru":"Греческая"}}]`,
		"categories.json": `[{"slug":"spices","names":{"en":"Spices","de":"Gewürze"}}]`,
	})
	l, err := LoadLabels(Layer(dir, seed))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ kind, key, lang, want string }{
		{Cuisine, "thai", "ru", "Тайская"},
		{Cuisine, "thai", "de", "Thai"}, // English fallback
		{Cuisine, "greek", "en", "Hellenic"},
		{Cuisine, "greek", "ru", "Греческая"},
		{Country, "us", "ru", "США"}, // codes are case-insensitive
		{Category, "spices", "de", "Gewürze"},
		{Cuisine, "middle-eastern", "en", "Middle eastern"}, // unknown → humanized
	}
	for _, c := range cases {
		if got, _ := l.Label(c.kind, c.key, c.lang); got != c.want {
			t.Errorf("%s/%s/%s = %q, want %q", c.kind, c.key, c.lang, got, c.want)
		}
	}
	if _, ok := l.Label(Cuisine, "nope", "en"); ok {
		t.Error("unknown key reported as known")
	}
	var nilLabels *Labels
	if got, _ := nilLabels.Label(Cuisine, "thai", "en"); got != "Thai" {
		t.Errorf("nil registry should humanize: %q", got)
	}
	if _, err := LoadLabels(fstest.MapFS{"countries.json": {Data: []byte("{")}}); err == nil {
		t.Error("broken file should fail")
	}
}

func TestLoadSite(t *testing.T) {
	seed := fstest.MapFS{"site.yaml": {Data: []byte("logo: \"🧭\"\nfeatures:\n  learning: true\n  goals: true\n")}}
	dir := writeFiles(t, map[string]string{"site.yaml": `
name: {en: Our Kitchen, ru: Наша кухня}
tagline: Cook what your shop sells
accent: "#c0392b"
logo: /static/logo.svg
features: {learning: false}
footer_links:
  - {label: About, url: https://example.org/about}
`})
	s, err := LoadSite(Layer(dir, seed))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name.In("ru") != "Наша кухня" || s.Name.In("de") != "Our Kitchen" || s.Tagline.In("ru") != "Cook what your shop sells" {
		t.Fatalf("texts: %+v %+v", s.Name, s.Tagline)
	}
	if s.On(FeatureLearning) || !s.On(FeatureGoals) || !s.On(FeatureShoppingList) {
		t.Fatalf("features merge across layers: %v", s.Features)
	}
	if !s.LogoImage() || s.Accent != "#c0392b" || len(s.FooterLinks) != 1 {
		t.Fatalf("site: %+v", s)
	}
	if d, err := LoadSite(fstest.MapFS{}); err != nil || !d.On(FeatureFavorites) || d.Logo == "" {
		t.Fatalf("defaults without site.yaml: %+v %v", d, err)
	}
	for _, bad := range []string{
		"accent: red; } body { display:none",
		"stylesheet: https://evil.example/x.css",
		"logo: /etc/../passwd",
		"features: {shoping_list: false}",
		"footer_links: [{label: x, url: 'javascript:alert(1)'}]",
		"name: [1, 2",
	} {
		if _, err := LoadSite(Layer(writeFiles(t, map[string]string{"site.yaml": bad}), seed)); err == nil {
			t.Errorf("site.yaml %q should be rejected", bad)
		}
	}
}
