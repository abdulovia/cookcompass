package shops

import "testing"

func TestSearchURLFor(t *testing.T) {
	sh := Shop{SearchURLTemplate: "https://lenta.com/search/?searchText={q}"}
	if got := sh.SearchURLFor("гречка крупа"); got != "https://lenta.com/search/?searchText=%D0%B3%D1%80%D0%B5%D1%87%D0%BA%D0%B0+%D0%BA%D1%80%D1%83%D0%BF%D0%B0" {
		t.Fatalf("query escaped: %s", got)
	}
	// {q} in the path, not just the query string.
	path := Shop{SearchURLTemplate: "https://groceries.asda.com/search/{q}"}
	if got := path.SearchURLFor("rice"); got != "https://groceries.asda.com/search/rice" {
		t.Fatalf("path placeholder: %s", got)
	}
	if got := (Shop{}).SearchURLFor("x"); got != "" {
		t.Fatalf("no template should give empty url, got %s", got)
	}
	// Every placeholder gets the query.
	one := Shop{SearchURLTemplate: "https://s.example/{q}?ref={q}"}
	if got := one.SearchURLFor("milk"); got != "https://s.example/milk?ref=milk" {
		t.Fatalf("replace all: %s", got)
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"  Lenta  ":   "lenta",
		"VkusVill":    "vkusvill",
		"Verhniy Mag": "verhniy-mag",
		"Озон":        "",
		"--x--":       "x",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := &Shop{CountryCode: "ru", Slug: "lenta", Name: "Лента", SearchURLTemplate: "https://lenta.com/s?text={q}", Active: true}
	if err := validate(ok); err != nil {
		t.Fatalf("valid shop rejected: %v", err)
	}
	if ok.CountryCode != "RU" {
		t.Fatalf("country not normalized: %s", ok.CountryCode)
	}
	// Nameless, bad country, template without {q}, non-http template.
	bad := &Shop{CountryCode: "RUS", Name: "", Website: "ftp://x", SearchURLTemplate: "https://s.example/search"}
	err := validate(bad)
	ve, okErr := err.(ValidationError)
	if !okErr || len(ve) != 5 {
		t.Fatalf("expected 5 errors, got %v", err)
	}
	for _, field := range []string{"name", "country", "slug", "website", "search_url_template"} {
		if _, ok := ve[field]; !ok {
			t.Errorf("missing error for %s in %v", field, ve)
		}
	}
	// Slug is derived from a latin name; a cyrillic-only name needs one.
	derived := &Shop{CountryCode: "RU", Name: "Spar"}
	if err := validate(derived); err != nil || derived.Slug != "spar" {
		t.Fatalf("slug derived: %v %q", err, derived.Slug)
	}
	cyr := &Shop{CountryCode: "RU", Name: "Пятёрочка"}
	if err := validate(cyr); err == nil {
		t.Fatal("cyrillic-only name without slug should fail")
	}
}
