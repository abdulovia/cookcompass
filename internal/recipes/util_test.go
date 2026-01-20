package recipes

import (
	"strings"
	"testing"
)

func TestContentKey(t *testing.T) {
	a := ContentKey("Fluffy Pancakes!", []string{"2 cups flour", "Milk", "eggs"})
	b := ContentKey("fluffy   pancakes", []string{"egg", "milk", "flour"})
	if a != b {
		t.Fatalf("keys should ignore case, order, plurals and punctuation:\n%s\n%s", a, b)
	}
	if !strings.HasPrefix(a, "fluffy pancakes|") {
		t.Fatalf("key prefix: %s", a)
	}
	c := ContentKey("Fluffy Pancakes", []string{"flour", "milk", "eggs", "butter"})
	if a == c {
		t.Fatal("different ingredients must change the key")
	}
}

func TestCanonicalizeURL(t *testing.T) {
	tests := map[string]string{
		"https://WWW.Example.com/recipes/pancakes/?utm_source=x&id=2#top": "https://example.com/recipes/pancakes?id=2",
		"http://example.com/a/":            "https://example.com/a",
		"ftp://example.com/a":              "",
		"not a url":                        "",
		"https://example.com/a?fbclid=123": "https://example.com/a",
	}
	for in, want := range tests {
		if got := CanonicalizeURL(in); got != want {
			t.Errorf("CanonicalizeURL(%q) = %q want %q", in, got, want)
		}
	}
	if d := Domain("https://www.Example.com/x"); d != "example.com" {
		t.Errorf("Domain = %q", d)
	}
	if d := Domain("://bad"); d != "" {
		t.Errorf("Domain of invalid = %q", d)
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Fluffy Pancakes":         "fluffy-pancakes",
		"Crème Brûlée":            "creme-brulee",
		"Борщ с говядиной":        "borsch-s-govyadinoy",
		"  !!!  ":                 "recipe",
		"Pad Thai (street-style)": "pad-thai-street-style",
		"Объём":                   "obem",
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q want %q", in, got, want)
		}
	}
	if got := Slugify(strings.Repeat("a", 100)); len(got) != 80 {
		t.Errorf("slug not truncated: %d", len(got))
	}
}

func TestYouTube(t *testing.T) {
	tests := []struct {
		in, id string
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://youtu.be/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/embed/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://youtube.com/shorts/dQw4w9WgXcQ", "dQw4w9WgXcQ"},
		{"https://m.youtube.com/watch?v=dQw4w9WgXcQ&t=10", "dQw4w9WgXcQ"},
		{"https://www.youtube.com/results?search_query=pancakes", ""},
		{"https://vimeo.com/123", ""},
		{"https://youtu.be/short", ""},
		{"https://youtu.be/dQw4w9WgX$Q", ""},
		{"", ""},
	}
	for _, tc := range tests {
		id, ok := YouTubeID(tc.in)
		if id != tc.id || ok != (tc.id != "") {
			t.Errorf("YouTubeID(%q) = %q,%v", tc.in, id, ok)
		}
		emb, ok := EmbedURL(tc.in)
		if ok != (tc.id != "") || (ok && emb != "https://www.youtube-nocookie.com/embed/"+tc.id) {
			t.Errorf("EmbedURL(%q) = %q", tc.in, emb)
		}
	}
	if !IsYouTubeURL("https://www.youtube.com/results?search_query=x") || IsYouTubeURL("https://evil.com/youtube.com") || IsYouTubeURL("javascript:alert(1)") {
		t.Error("IsYouTubeURL misclassified")
	}
}

func TestDiff(t *testing.T) {
	a := "a\nb\nc\nd"
	b := "a\nc\nd\ne"
	got := Diff(a, b)
	var sb strings.Builder
	for _, l := range got {
		sb.WriteString(l.Prefix() + l.Text + "|" + l.Class() + "\n")
	}
	want := " a|same\n-b|del\n c|same\n d|same\n+e|add\n"
	if sb.String() != want {
		t.Fatalf("diff:\n%s\nwant:\n%s", sb.String(), want)
	}
	if !DiffChanged(got) || DiffChanged(Diff(a, a)) {
		t.Fatal("DiffChanged wrong")
	}
	if len(Diff("", "x")) != 1 || len(Diff("x", "")) != 1 {
		t.Fatal("empty side diff")
	}
}

func validRecipe() *Recipe {
	return &Recipe{
		Name: " Toast ", Difficulty: Easy, MealTimes: []string{"snack", "Breakfast", "breakfast"}, Servings: 1,
		Steps:       []string{"Toast the bread.", "  "},
		Ingredients: []Ingredient{{Name: "bread", Essential: true}, {Name: " "}},
		Nutrition:   Nutrition{Calories: 100},
	}
}

func TestValidate(t *testing.T) {
	r := validRecipe()
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if r.Name != "Toast" || len(r.Steps) != 1 || len(r.Ingredients) != 1 {
		t.Fatalf("normalization failed: %+v", r)
	}
	if strings.Join(r.MealTimes, ",") != "breakfast,snack" {
		t.Fatalf("meal times %v", r.MealTimes)
	}

	bad := &Recipe{Name: strings.Repeat("x", 201), Difficulty: "extreme", MealTimes: []string{"brunch"}, Servings: 0,
		PrepMinutes: -1, CookMinutes: 99999, VideoURL: "https://vimeo.com/1", ImageURL: "nope",
		Nutrition: Nutrition{Calories: -5}, Translations: map[string]Translation{"toolong": {Name: "x"}}}
	err := bad.Validate()
	ve, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("want ValidationError, got %v", err)
	}
	for _, f := range []string{"name", "difficulty", "meal_times", "servings", "prep_minutes", "cook_minutes",
		"video_url", "image_url", "calories", "steps", "ingredients", "translations"} {
		if ve[f] == "" {
			t.Errorf("missing error for %s (%v)", f, ve)
		}
	}
	if !strings.Contains(err.Error(), "difficulty:") {
		t.Errorf("Error() = %s", err)
	}
	empty := &Recipe{}
	if ve := empty.Validate().(ValidationError); ve["name"] != "validation.required" || ve["meal_times"] != "validation.required" {
		t.Errorf("empty recipe errors %v", ve)
	}
}

func TestRecipeHelpers(t *testing.T) {
	r := validRecipe()
	r.PrepMinutes, r.CookMinutes = 5, 10
	r.Translations = map[string]Translation{"ru": {Name: "Тост", Steps: []string{"Поджарьте хлеб."}, Ingredients: []string{"хлеб"}}}
	if r.TotalMinutes() != 15 || !r.HasMealTime("snack") || r.HasMealTime("dinner") || r.Deleted() || r.IsScraped() {
		t.Fatal("helpers wrong")
	}
	ru := r.Localized("ru")
	if ru.Name != "Тост" || ru.Steps[0] != "Поджарьте хлеб." || r.Name == "Тост" {
		t.Fatal("localization wrong or mutated original")
	}
	if r.Localized("de").Name != r.Name {
		t.Fatal("fallback wrong")
	}
	if r.IngredientLine("ru", 0) != "хлеб" || r.IngredientLine("ru", 5) != "" || r.IngredientLine("de", 0) != "" {
		t.Fatal("IngredientLine wrong")
	}
	r.Source = ScrapedSource("example.com")
	if !r.IsScraped() || r.Source != "scraped:example.com" {
		t.Fatal("scraped source wrong")
	}
	n := Nutrition{Calories: 100, Protein: 10}.Scale(2).Add(Nutrition{Calories: 1})
	if n.Calories != 201 || n.Protein != 20 {
		t.Fatalf("nutrition math %+v", n)
	}
	if got := ParseMealTimes(" lunch, ,dinner"); len(got) != 2 {
		t.Fatalf("ParseMealTimes %v", got)
	}
}
