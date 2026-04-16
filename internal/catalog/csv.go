package catalog

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// CSVHeader is the canonical bulk-edit format for products. Only "name" is
// required; unknown columns are ignored so spreadsheets can carry notes.
// "shop" is a shops.csv slug and namespaces the dedup key; "url" is the
// exact product page used for buy links.
var CSVHeader = []string{"sku", "name", "category", "ingredient", "shop", "url", "price", "unit"}

// ReadCSV parses products for country from r.
func ReadCSV(r io.Reader, country string) ([]*Product, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\uFEFF")))] = i
	}
	if _, ok := col["name"]; !ok {
		return nil, errors.New(`csv: missing required column "name"`)
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	var out []*Product
	line := 1
	for {
		rec, err := cr.Read()
		line++
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if strings.Join(rec, "") == "" {
			continue
		}
		p := &Product{
			CountryCode:    strings.ToUpper(country),
			SKU:            get(rec, "sku"),
			Name:           get(rec, "name"),
			Category:       get(rec, "category"),
			IngredientSlug: get(rec, "ingredient"),
			ShopSlug:       get(rec, "shop"),
			URL:            get(rec, "url"),
			Unit:           get(rec, "unit"),
			Currency:       get(rec, "currency"),
		}
		if c := get(rec, "country"); c != "" {
			p.CountryCode = strings.ToUpper(c)
		}
		if v := get(rec, "price"); v != "" {
			f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: price %q: %w", line, v, err)
			}
			p.Price = f
		}
		if p.Name == "" {
			return nil, fmt.Errorf("line %d: name is required", line)
		}
		out = append(out, p)
	}
	return out, nil
}

// WriteCSV writes products in the canonical format.
func WriteCSV(w io.Writer, products []*Product) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(CSVHeader); err != nil {
		return err
	}
	for _, p := range products {
		if err := cw.Write([]string{p.SKU, p.Name, p.Category, p.IngredientSlug, p.ShopSlug, p.URL,
			strconv.FormatFloat(p.Price, 'f', -1, 64), p.Unit}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
