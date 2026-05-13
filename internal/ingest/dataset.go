package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// DatasetSource imports recipe datasets: schema.org Recipe objects packed as
// a JSON array, a JSONL stream or one object per line. Files land in the
// recipe inbox directory (see docs/ingestion.md); URLs are fetched politely
// through the shared client and therefore capped at fetch.MaxBody — drop
// larger dumps into the inbox instead.
type DatasetSource struct {
	Fetcher *fetch.Client
	// Dir is the inbox directory scanned when no explicit URLs are given.
	Dir string
	Log *slog.Logger
}

// Name implements recipes.RecipeSource.
func (s *DatasetSource) Name() string { return "dataset" }

// Fetch implements recipes.RecipeSource.
func (s *DatasetSource) Fetch(ctx context.Context, opts recipes.FetchOpts) ([]recipes.RawRecipe, error) {
	targets := opts.URLs
	if len(targets) == 0 && s.Dir != "" {
		entries, err := os.ReadDir(s.Dir)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".json", ".jsonl":
				targets = append(targets, "file:"+filepath.Join(s.Dir, e.Name()))
			}
		}
		sort.Strings(targets)
	}
	var out []recipes.RawRecipe
	var errs []error
	for _, u := range targets {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		body, err := s.Fetcher.Get(ctx, u)
		if err != nil {
			errs = append(errs, err)
			if s.Log != nil {
				s.Log.Warn("skip dataset", "url", u, "err", err)
			}
			continue
		}
		found := ExtractDataset(body)
		if len(found) == 0 && s.Log != nil {
			s.Log.Info("no recipes in dataset", "url", u)
		}
		for i := range found {
			found[i].URL = u
		}
		out = append(out, found...)
		if opts.Limit > 0 && len(out) >= opts.Limit {
			return out[:opts.Limit], nil
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ExtractDataset finds recipes in a dataset payload: any sequence of JSON
// values — a pretty-printed array, one object per line, multi-line objects
// or a mix. Objects typed Recipe use the schema mapping; untyped items
// shaped like a recipe (a name plus ingredients) are accepted too, which is
// how the OpenRecipes dump stores them. A broken value stops the scan, like
// a truncated download would.
func ExtractDataset(data []byte) []recipes.RawRecipe {
	var out []recipes.RawRecipe
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			return out // io.EOF, or the rest of the payload is broken
		}
		for _, node := range datasetRecipes(v) {
			raw := toRaw(node)
			if raw.Name != "" {
				out = append(out, raw)
			}
		}
	}
}

// datasetRecipes walks dataset JSON looking for recipe objects: anything
// typed Recipe, or an untyped object with a name and an ingredient list.
func datasetRecipes(v any) []map[string]any {
	var out []map[string]any
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			out = append(out, datasetRecipes(x)...)
		}
	case map[string]any:
		switch {
		case hasType(t, "Recipe"):
			return append(out, t)
		case recipeShaped(t):
			return append(out, t)
		}
		for _, k := range []string{"@graph", "mainEntity", "itemListElement", "item", "recipes", "data"} {
			if x, ok := t[k]; ok {
				out = append(out, datasetRecipes(x)...)
			}
		}
	}
	return out
}

// recipeShaped reports whether an untyped object looks like a recipe record.
func recipeShaped(m map[string]any) bool {
	if str(m["name"]) == "" {
		return false
	}
	_, hasIngredients := m["ingredients"]
	_, hasRecipeIngredient := m["recipeIngredient"]
	return hasIngredients || hasRecipeIngredient
}

// InboxFiles lists importable dataset files in dir for the admin form.
func InboxFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".json", ".jsonl":
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
