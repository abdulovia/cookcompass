// Package web renders HTML pages and provides request-scoped helpers
// (language, flash messages, form errors) shared by public and admin handlers.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"path"
	"strings"

	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Renderer executes page templates. Each page is parsed together with the
// shared layout and partials so pages can define their own blocks.
type Renderer struct {
	pages map[string]*template.Template
}

// NewRenderer parses templates from fsys (rooted at the web/ directory).
func NewRenderer(fsys fs.FS) (*Renderer, error) {
	base, err := template.New("").Funcs(Funcs()).ParseFS(fsys, "templates/layout.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}
	r := &Renderer{pages: map[string]*template.Template{}}
	for _, dir := range []string{"pages", "admin"} {
		files, err := fs.Glob(fsys, "templates/"+dir+"/*.html")
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			t, err := base.Clone()
			if err != nil {
				return nil, err
			}
			if _, err := t.ParseFS(fsys, f); err != nil {
				return nil, fmt.Errorf("parse %s: %w", f, err)
			}
			r.pages[dir+"/"+strings.TrimSuffix(path.Base(f), ".html")] = t
		}
	}
	return r, nil
}

// Has reports whether a page exists.
func (r *Renderer) Has(name string) bool { _, ok := r.pages[name]; return ok }

// Page renders a full page through the layout.
func (r *Renderer) Page(w http.ResponseWriter, status int, name string, p *Page) {
	r.execute(w, status, name, "layout", p)
}

// Partial renders one named block of a page (used for HTMX swaps).
func (r *Renderer) Partial(w http.ResponseWriter, status int, name, block string, p *Page) {
	r.execute(w, status, name, block, p)
}

func (r *Renderer) execute(w http.ResponseWriter, status int, name, block string, p *Page) {
	t, ok := r.pages[name]
	if !ok {
		http.Error(w, "template "+name+" not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, block, p); err != nil {
		http.Error(w, "render "+name+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// Funcs are the template helpers.
func Funcs() template.FuncMap {
	return template.FuncMap{
		"qty": recipes.FormatQuantity,
		"num": func(f float64) string {
			if f == math.Trunc(f) {
				return fmt.Sprintf("%.0f", f)
			}
			return fmt.Sprintf("%.1f", f)
		},
		"money": func(f float64) string { return fmt.Sprintf("%.2f", f) },
		"pct": func(part, whole float64) int {
			if whole <= 0 {
				return 0
			}
			return int(math.Round(math.Min(part/whole, 1.5) * 100))
		},
		"add":   func(a, b int) int { return a + b },
		"sub":   func(a, b int) int { return a - b },
		"embed": func(u string) string { e, _ := recipes.EmbedURL(u); return e },
		"join":  strings.Join,
		"has": func(list []string, v string) bool {
			for _, x := range list {
				if x == v {
					return true
				}
			}
			return false
		},
		"json": func(v any) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"dict": func(kv ...any) map[string]any {
			m := make(map[string]any, len(kv)/2)
			for i := 0; i+1 < len(kv); i += 2 {
				m[fmt.Sprint(kv[i])] = kv[i+1]
			}
			return m
		},
		"seq": func(n int) []int {
			s := make([]int, max(n, 0))
			for i := range s {
				s[i] = i
			}
			return s
		},
		"lower": strings.ToLower,
		"list":  func(items ...string) []string { return items },
	}
}
