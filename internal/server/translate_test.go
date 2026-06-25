package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeLibreTranslate prefixes every text with its target locale, like a
// very honest machine translator.
func fakeLibreTranslate(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/translate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Q      []string `json:"q"`
			Source string   `json:"source"`
			Target string   `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type pair struct {
			TranslatedText string `json:"translatedText"`
		}
		out := make([]pair, len(req.Q))
		for i, q := range req.Q {
			out[i] = pair{TranslatedText: "[" + req.Target + "] " + q}
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTranslateDraft(t *testing.T) {
	lt := fakeLibreTranslate(t)
	h := newHarness(t)
	h.app.Cfg.Translate.URL = lt.URL

	rec, err := h.app.Recipes.GetBySlug(context.Background(), "pad-thai")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.TranslateRecipeDraft(context.Background(), rec, "es"); err != nil {
		t.Fatalf("draft: %v", err)
	}
	fresh, _ := h.app.Recipes.GetBySlug(context.Background(), "pad-thai")
	tr, ok := fresh.Translations["es"]
	if !ok || !strings.HasPrefix(tr.Name, "[es] ") || len(tr.Steps) != len(fresh.Steps) ||
		len(tr.Ingredients) != len(fresh.Ingredients) {
		t.Fatalf("draft translation wrong: %+v", tr)
	}
	// The revision history records the machine draft for reviewers.
	if _, body := h.get("/admin/recipes/" + itoa(fresh.ID) + "/revisions"); !strings.Contains(body, "machine translation draft (es)") {
		t.Fatal("revision should record the machine draft")
	}

	// The public page shows the drafted locale.
	if _, body := h.get("/recipes/pad-thai", "Accept-Language", "es"); !strings.Contains(body, "[es] ") {
		t.Fatal("es page should show the draft")
	}

	// The admin button appears only with a configured translator.
	_, body := h.get("/admin/recipes/" + itoa(fresh.ID) + "/edit")
	if !strings.Contains(body, "Draft translation") && !strings.Contains(body, "translate") {
		t.Fatal("draft button missing")
	}
	form := url.Values{"locale": {"fr"}}
	if resp, _ := h.post("/admin/recipes/"+itoa(fresh.ID)+"/translate", form); !strings.Contains(resp.Header.Get("Location"), "translated") {
		t.Fatalf("translate: %s", resp.Header.Get("Location"))
	}
}

func TestTranslateNotConfigured(t *testing.T) {
	h := newHarness(t)
	rec, _ := h.app.Recipes.GetBySlug(context.Background(), "pad-thai")
	if err := h.app.TranslateRecipeDraft(context.Background(), rec, "de"); err == nil {
		t.Fatal("expected error without COOK_TRANSLATE_URL")
	}
	if resp, _ := h.post("/admin/recipes/"+itoa(rec.ID)+"/translate", url.Values{"locale": {"de"}}); !strings.Contains(resp.Header.Get("Location"), "translate_failed") {
		t.Fatalf("unconfigured: %s", resp.Header.Get("Location"))
	}
}
