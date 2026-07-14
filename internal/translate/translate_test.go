package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		n    int
		want []string
		err  string
	}{
		{"batch", `{"translatedText":["Hola","Mundo"]}`, 2, []string{"Hola", "Mundo"}, ""},
		{"single", `{"translatedText":"Hola"}`, 1, []string{"Hola"}, ""},
		{"legacy array", `[{"translatedText":"Hola"},{"translatedText":"Mundo"}]`, 2, []string{"Hola", "Mundo"}, ""},
		{"count mismatch", `{"translatedText":["Hola"]}`, 2, nil, "got 1 results for 2"},
		{"error field", `{"error":"ru is not supported"}`, 1, nil, "ru is not supported"},
		{"garbage", `<html>`, 1, nil, "unexpected response"},
	}
	for _, c := range cases {
		got, err := decode([]byte(c.body), c.n)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err %v, want %q", c.name, err, c.err)
			}
			continue
		}
		if err != nil || strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: got %q, %v", c.name, got, err)
		}
	}
}

// TestTranslateBatches speaks the real LibreTranslate batch shape and checks
// that inputs over the batch size keep their order.
func TestTranslateBatches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Target != "ru" || req.Format != "text" {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		out := make([]string, len(req.Q))
		for i, q := range req.Q {
			out[i] = "ru:" + q
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"translatedText": out})
	}))
	defer srv.Close()

	in := make([]string, 45)
	for i := range in {
		in[i] = strings.Repeat("x", i)
	}
	got, err := New(srv.URL+"/").Translate(context.Background(), "en", "ru", in...)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(got) != len(in) || got[44] != "ru:"+in[44] || got[0] != "ru:" {
		t.Fatalf("calls=%d len=%d last=%q", calls, len(got), got[len(got)-1])
	}
	if _, err := New(srv.URL).Translate(context.Background(), "en", "xx", "a"); err == nil ||
		!strings.Contains(err.Error(), "bad request") {
		t.Fatalf("error status: %v", err)
	}
	if New("  ") != nil {
		t.Fatal("blank URL should disable the client")
	}
}
