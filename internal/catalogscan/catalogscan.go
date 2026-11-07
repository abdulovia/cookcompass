// Package catalogscan refreshes per-country product catalogs from pluggable
// sources (shop pages with schema.org Product markup, offline CSV/JSON
// drops, the bundled seed) into the review queue, with the same dedup and
// "user edits win" rules as recipe ingestion.
package catalogscan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/fetch"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// CatalogSource yields products sold in a country.
type CatalogSource interface {
	Name() string
	Fetch(ctx context.Context, country string) ([]catalog.Product, error)
}

// ---------- seed (bundled, always available) ----------

// SeedSource reads the bundled seed CSVs. It is the offline fallback.
type SeedSource struct{ FS fs.FS }

// Name implements CatalogSource.
func (s *SeedSource) Name() string { return "seed" }

// Fetch implements CatalogSource.
func (s *SeedSource) Fetch(_ context.Context, country string) ([]catalog.Product, error) {
	f, err := s.FS.Open("products/" + strings.ToLower(country) + ".csv")
	if err != nil {
		return nil, fmt.Errorf("no seed catalog for %s: %w", country, err)
	}
	defer f.Close()
	list, err := catalog.ReadCSV(f, country)
	if err != nil {
		return nil, err
	}
	out := make([]catalog.Product, len(list))
	for i, p := range list {
		p.Source = "seed"
		p.Status = catalog.StatusApproved
		out[i] = *p
	}
	return out, nil
}

// ---------- file inbox (offline / in-store catalogs) ----------

// FileSource imports CSV or JSON catalog files dropped into
// <Dir>/<COUNTRY>/ (e.g. a supermarket's export or a hand-made list of
// what the corner shop stocks). See docs/examples/catalog.json and
// docs/schemas/catalog.schema.json.
type FileSource struct{ Dir string }

// Name implements CatalogSource.
func (s *FileSource) Name() string { return "file" }

// JSONCatalog is the JSON import format.
type JSONCatalog struct {
	Country  string        `json:"country"`
	Store    string        `json:"store,omitempty"`
	Currency string        `json:"currency,omitempty"`
	Products []JSONProduct `json:"products"`
}

// JSONProduct is one product in a JSON catalog.
type JSONProduct struct {
	SKU        string  `json:"sku,omitempty"`
	Name       string  `json:"name"`
	Category   string  `json:"category,omitempty"`
	Ingredient string  `json:"ingredient,omitempty"`
	Price      float64 `json:"price,omitempty"`
	Unit       string  `json:"unit,omitempty"`
}

// Fetch implements CatalogSource.
func (s *FileSource) Fetch(_ context.Context, country string) ([]catalog.Product, error) {
	dir := filepath.Join(s.Dir, strings.ToUpper(country))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []catalog.Product
	var errs []error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name())
		list, err := ReadCatalogFile(p, country)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, list...)
	}
	return out, errors.Join(errs...)
}

// ReadCatalogFile parses a CSV or JSON catalog file.
func ReadCatalogFile(p, country string) ([]catalog.Product, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	tag := "file:" + filepath.Base(p)
	var list []catalog.Product
	switch strings.ToLower(filepath.Ext(p)) {
	case ".csv":
		ps, err := catalog.ReadCSV(bytes.NewReader(b), country)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		for _, x := range ps {
			list = append(list, *x)
		}
	case ".json":
		var jc JSONCatalog
		if err := json.Unmarshal(b, &jc); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if jc.Country != "" && !strings.EqualFold(jc.Country, country) {
			return nil, fmt.Errorf("%s: catalog is for %s, not %s", p, jc.Country, country)
		}
		if jc.Store != "" {
			tag = "file:" + jc.Store
		}
		for _, jp := range jc.Products {
			if strings.TrimSpace(jp.Name) == "" {
				return nil, fmt.Errorf("%s: product without name", p)
			}
			list = append(list, catalog.Product{CountryCode: strings.ToUpper(country), SKU: jp.SKU, Name: jp.Name,
				Category: jp.Category, IngredientSlug: jp.Ingredient, Price: jp.Price, Unit: jp.Unit, Currency: jc.Currency})
		}
	default:
		return nil, fmt.Errorf("%s: unsupported file type (use .csv or .json)", p)
	}
	for i := range list {
		list[i].Source = tag
		list[i].CountryCode = strings.ToUpper(country)
	}
	return list, nil
}

// ---------- shop pages with schema.org Product JSON-LD ----------

// JSONLDSource reads product pages listed per country in a targets file
// ("<CC> <URL>" per line) through the polite fetcher.
type JSONLDSource struct {
	Fetcher *fetch.Client
	Targets map[string][]string
	Log     *slog.Logger
}

// Name implements CatalogSource.
func (s *JSONLDSource) Name() string { return "jsonld" }

// Fetch implements CatalogSource.
func (s *JSONLDSource) Fetch(ctx context.Context, country string) ([]catalog.Product, error) {
	var out []catalog.Product
	var errs []error
	for _, u := range s.Targets[strings.ToUpper(country)] {
		body, err := s.Fetcher.Get(ctx, u)
		if err != nil {
			errs = append(errs, err)
			if s.Log != nil {
				s.Log.Warn("skip catalog page", "url", u, "err", err)
			}
			continue
		}
		for _, p := range ExtractProducts(body, u) {
			p.CountryCode = strings.ToUpper(country)
			out = append(out, p)
		}
	}
	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// ReadTargets parses "<COUNTRY> <URL>" lines; "#" starts a comment.
func ReadTargets(p string) (map[string][]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][]string{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cc, u, ok := strings.Cut(line, " ")
		cc = strings.ToUpper(strings.TrimSpace(cc))
		u = strings.TrimSpace(u)
		if !ok || len(cc) != 2 || u == "" {
			return nil, fmt.Errorf("%s:%d: want \"<COUNTRY> <URL>\"", p, n)
		}
		if strings.HasPrefix(u, "file:") && !filepath.IsAbs(strings.TrimPrefix(u, "file:")) {
			u = "file:" + filepath.Join(filepath.Dir(p), strings.TrimPrefix(u, "file:"))
		}
		out[cc] = append(out[cc], u)
	}
	return out, sc.Err()
}

// ExtractProducts finds schema.org Product objects (with their Offer) in
// the JSON-LD blocks of a page.
func ExtractProducts(page []byte, pageURL string) []catalog.Product {
	var out []catalog.Product
	domain := recipes.Domain(pageURL)
	if domain == "" {
		domain = "local"
	}
	for _, block := range ldBlocks(page) {
		var v any
		if json.Unmarshal(block, &v) != nil {
			continue
		}
		for _, m := range findTyped(v, "Product") {
			p := catalog.Product{
				Name:      str(m["name"]),
				SKU:       firstNonEmpty(str(m["sku"]), str(m["gtin13"]), str(m["gtin"]), str(m["productID"])),
				Category:  lastSegment(str(m["category"])),
				SourceURL: firstNonEmpty(str(m["url"]), pageURL),
				Source:    "scraped:" + domain,
			}
			if offer := firstMap(m["offers"]); offer != nil {
				p.Price = num(offer["price"])
				p.Currency = str(offer["priceCurrency"])
				if p.Price == 0 {
					p.Price = num(offer["lowPrice"])
				}
			}
			if size := str(m["size"]); size != "" {
				p.Unit = size
			} else if w := firstMap(m["weight"]); w != nil {
				p.Unit = strings.TrimSpace(str(w["value"]) + " " + str(w["unitText"]))
			}
			if p.Name != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func ldBlocks(page []byte) [][]byte {
	var out [][]byte
	z := xhtml.NewTokenizer(bytes.NewReader(page))
	in := false
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return out
		case xhtml.StartTagToken:
			name, more := z.TagName()
			in = false
			if string(name) == "script" {
				for more {
					var k, v []byte
					k, v, more = z.TagAttr()
					if string(k) == "type" && strings.EqualFold(strings.TrimSpace(string(v)), "application/ld+json") {
						in = true
					}
				}
			}
		case xhtml.TextToken:
			if in {
				out = append(out, append([]byte(nil), z.Text()...))
			}
		case xhtml.EndTagToken:
			in = false
		}
	}
}

func findTyped(v any, typ string) []map[string]any {
	var out []map[string]any
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			out = append(out, findTyped(x, typ)...)
		}
	case map[string]any:
		for _, ty := range toStrings(t["@type"]) {
			if strings.EqualFold(ty, typ) {
				return append(out, t)
			}
		}
		for _, k := range []string{"@graph", "itemListElement", "item", "mainEntity"} {
			if x, ok := t[k]; ok {
				out = append(out, findTyped(x, typ)...)
			}
		}
	}
	return out
}

func firstMap(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	case []any:
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

var reNum = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(strings.ReplaceAll(reNum.FindString(t), ",", "."), 64)
		return f
	}
	return 0
}

func lastSegment(cat string) string {
	if i := strings.LastIndexAny(cat, ">/"); i >= 0 {
		cat = cat[i+1:]
	}
	return strings.ToLower(strings.TrimSpace(cat))
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---------- scanner ----------

// Result summarizes a scan for one country.
type Result struct {
	Country  string
	Sources  []string
	Fetched  int
	Created  int
	Updated  int
	Skipped  int
	Fallback bool // nothing could be fetched; seed catalog was re-applied
	Errors   []error
}

func (r Result) String() string {
	return fmt.Sprintf("%s: sources=%s fetched=%d created=%d updated=%d skipped=%d fallback=%v",
		r.Country, strings.Join(r.Sources, ","), r.Fetched, r.Created, r.Updated, r.Skipped, r.Fallback)
}

// Scanner runs sources for a country and stores what they find.
type Scanner struct {
	Catalog *catalog.Store
	Sources []CatalogSource // tried in order; all results are merged
	Seed    CatalogSource   // fallback when the others yield nothing
	Log     *slog.Logger
}

// Scan refreshes one country. New items from external sources land as
// pending for review; products edited by a user are never overwritten.
func (s *Scanner) Scan(ctx context.Context, country string) Result {
	country = strings.ToUpper(country)
	res := Result{Country: country}
	var found []catalog.Product
	for _, src := range s.Sources {
		list, err := src.Fetch(ctx, country)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", src.Name(), err))
		}
		if len(list) > 0 {
			res.Sources = append(res.Sources, src.Name())
		}
		found = append(found, list...)
	}
	onlyNew := false
	if len(found) == 0 && s.Seed != nil {
		list, err := s.Seed.Fetch(ctx, country)
		if err != nil {
			res.Errors = append(res.Errors, err)
		}
		found, res.Fallback, onlyNew = list, true, true
		res.Sources = append(res.Sources, s.Seed.Name())
		if s.Log != nil {
			s.Log.Warn("catalog sources returned nothing; using bundled seed", "country", country)
		}
	}
	res.Fetched = len(found)
	for i := range found {
		p := found[i]
		p.CountryCode = country
		if p.Status == "" {
			p.Status = catalog.StatusPending
		}
		out, err := s.Catalog.UpsertProduct(ctx, &p, onlyNew)
		switch {
		case err != nil:
			res.Skipped++
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", p.Name, err))
		case out == catalog.Created:
			res.Created++
		case out == catalog.Updated:
			res.Updated++
		default:
			res.Skipped++
		}
	}
	if s.Log != nil {
		s.Log.Info("catalog scan", "result", res.String())
	}
	return res
}

// Countries returns the codes that have a seed catalog in fsys.
func Countries(fsys fs.FS) []string {
	files, _ := fs.Glob(fsys, "products/*.csv")
	var out []string
	for _, f := range files {
		out = append(out, strings.ToUpper(strings.TrimSuffix(path.Base(f), ".csv")))
	}
	sort.Strings(out)
	return out
}
