package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLanguageSelection(t *testing.T) {
	h := newHarness(t)

	// Accept-Language picks Russian: UI and recipe content are translated.
	_, body := h.get("/recipes/syrniki", "Accept-Language", "ru-RU,ru;q=0.9,en;q=0.5")
	for _, want := range []string{`<html lang="ru">`, "Сырники", "Ингредиенты", "500 г творога", "Жарьте сырники", "Все рецепты"} {
		if !strings.Contains(body, want) && !strings.Contains(body, strings.ToLower(want)) {
			t.Errorf("ru detail missing %q", want)
		}
	}
	_, body = h.get("/recipes?f=1&cuisine=russian", "Accept-Language", "ru")
	if !strings.Contains(body, "5 рецептов") || !strings.Contains(body, "Пельмени") || !strings.Contains(body, "Русская") {
		t.Fatalf("ru browse: %s", snippet(body, "рецепт"))
	}
	// Scraped/user recipes without a Russian translation fall back to English text
	// with Russian unit labels and ingredient names.
	if _, body = h.get("/api/recipes/fluffy-pancakes", "Accept-Language", "ru"); !strings.Contains(body, "Пышные панкейки") {
		t.Fatal("api should localize")
	}

	// The header switcher stores a cookie that beats Accept-Language.
	resp, _ := h.post("/lang", url.Values{"lang": {"de"}, "back": {"/recipes"}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/recipes" {
		t.Fatalf("switch: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body = h.get("/recipes", "Accept-Language", "ru")
	if !strings.Contains(body, `<html lang="de">`) || !strings.Contains(body, "Rezepte") || !strings.Contains(body, "Nur was ich gerade kochen kann") {
		t.Fatal("German locale should be fully translated")
	}
	// Unknown languages are ignored.
	h.post("/lang", url.Values{"lang": {"xx"}})
	if _, body = h.get("/"); !strings.Contains(body, `<html lang="de">`) {
		t.Fatal("unknown language replaced the cookie")
	}

	// Settings language is stored on the profile and applies without the cookie.
	fresh := newHarness(t)
	fresh.post("/settings", url.Values{"country": {"RU"}, "units": {"metric"}, "lang": {"ru"}})
	fresh.client.Jar = nil
	if _, body = fresh.get("/"); !strings.Contains(body, `<html lang="ru">`) || !strings.Contains(body, "С чего начать") {
		t.Fatal("profile language not applied")
	}
	if _, body = fresh.get("/settings"); !strings.Contains(body, "Deutsch") || strings.Contains(body, "частично") {
		t.Fatal("complete languages should not be labelled partial in settings")
	}
	if _, body = fresh.get("/does-not-exist"); !strings.Contains(body, "Страница не найдена") {
		t.Fatal("404 page in Russian")
	}
}
