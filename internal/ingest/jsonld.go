// Package ingest turns pages from the outside world into reviewed recipes:
// sources fetch raw recipes, the normalizer maps them onto the domain model,
// and the importer deduplicates them into the review queue.
package ingest

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/abdulovia/cookcompass/internal/recipes"
)

// ExtractJSONLD finds every schema.org Recipe in the JSON-LD blocks of an
// HTML page. Malformed blocks are skipped; pageURL is used as the fallback
// canonical URL.
func ExtractJSONLD(page []byte, pageURL string) []recipes.RawRecipe {
	blocks, canonical := scanPage(page)
	if canonical == "" {
		canonical = pageURL
	}
	var out []recipes.RawRecipe
	for _, b := range blocks {
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			continue
		}
		for _, node := range findRecipes(v) {
			raw := toRaw(node)
			raw.URL = pageURL
			if raw.CanonicalURL == "" {
				raw.CanonicalURL = canonical
			}
			if raw.Name != "" {
				out = append(out, raw)
			}
		}
	}
	return out
}

// scanPage returns JSON-LD script contents and the <link rel=canonical> href.
func scanPage(page []byte) (blocks [][]byte, canonical string) {
	z := xhtml.NewTokenizer(bytes.NewReader(page))
	inLD := false
	for {
		switch z.Next() {
		case xhtml.ErrorToken:
			return blocks, canonical
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
			}
			switch string(name) {
			case "script":
				inLD = strings.EqualFold(strings.TrimSpace(attrs["type"]), "application/ld+json")
			case "link":
				if strings.EqualFold(attrs["rel"], "canonical") && canonical == "" {
					canonical = attrs["href"]
				}
			}
		case xhtml.TextToken:
			if inLD {
				blocks = append(blocks, append([]byte(nil), z.Text()...))
			}
		case xhtml.EndTagToken:
			inLD = false
		}
	}
}

// findRecipes walks arbitrary JSON-LD looking for objects typed Recipe,
// including inside @graph and nested arrays.
func findRecipes(v any) []map[string]any {
	var out []map[string]any
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			out = append(out, findRecipes(x)...)
		}
	case map[string]any:
		if hasType(t, "Recipe") {
			out = append(out, t)
			return out
		}
		for _, k := range []string{"@graph", "mainEntity", "itemListElement", "item"} {
			if x, ok := t[k]; ok {
				out = append(out, findRecipes(x)...)
			}
		}
	}
	return out
}

func hasType(m map[string]any, typ string) bool {
	for _, t := range strs(m["@type"]) {
		if strings.EqualFold(t, typ) || strings.HasSuffix(t, "/"+typ) {
			return true
		}
	}
	return false
}

func toRaw(m map[string]any) recipes.RawRecipe {
	r := recipes.RawRecipe{
		Source:       "jsonld",
		Name:         clean(str(m["name"])),
		Description:  clean(str(m["description"])),
		Ingredients:  cleanAll(strs(firstOf(m, "recipeIngredient", "ingredients"))),
		Steps:        instructions(m["recipeInstructions"]),
		Cuisines:     cleanAll(strs(m["recipeCuisine"])),
		Categories:   cleanAll(strs(m["recipeCategory"])),
		Keywords:     keywords(m["keywords"]),
		PrepMinutes:  ParseDuration(str(m["prepTime"])),
		CookMinutes:  ParseDuration(str(m["cookTime"])),
		TotalMinutes: ParseDuration(str(m["totalTime"])),
		Yield:        clean(firstStr(m["recipeYield"])),
		VideoURL:     mediaURL(m["video"], "embedUrl", "contentUrl", "url"),
		ImageURL:     mediaURL(m["image"], "url", "contentUrl"),
		Nutrition:    nutrition(m["nutrition"]),
		Language:     str(m["inLanguage"]),
	}
	switch id := m["mainEntityOfPage"].(type) {
	case string:
		r.CanonicalURL = id
	case map[string]any:
		r.CanonicalURL = str(id["@id"])
	}
	if r.CanonicalURL == "" {
		r.CanonicalURL = str(m["url"])
	}
	if !strings.HasPrefix(r.CanonicalURL, "http") {
		r.CanonicalURL = ""
	}
	return r
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

// instructions flattens strings, HowToStep and HowToSection lists.
func instructions(v any) []string {
	var out []string
	switch t := v.(type) {
	case string:
		out = append(out, splitInstructions(t)...)
	case []any:
		for _, x := range t {
			out = append(out, instructions(x)...)
		}
	case map[string]any:
		if items, ok := t["itemListElement"]; ok {
			return instructions(items)
		}
		if s := clean(str(t["text"])); s != "" {
			out = append(out, s)
		} else if s := clean(str(t["name"])); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var reStepNumber = regexp.MustCompile(`^\s*(?:step\s*)?\d+[.):]\s*`)

func splitInstructions(s string) []string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = clean(reStepNumber.ReplaceAllString(line, ""))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func keywords(v any) []string {
	var out []string
	for _, s := range strs(v) {
		for _, k := range strings.Split(s, ",") {
			if k = clean(k); k != "" {
				out = append(out, k)
			}
		}
	}
	return out
}

func mediaURL(v any, keys ...string) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		for _, x := range t {
			if u := mediaURL(x, keys...); u != "" {
				return u
			}
		}
	case map[string]any:
		for _, k := range keys {
			if s := str(t[k]); s != "" {
				return s
			}
		}
	}
	return ""
}

func nutrition(v any) *recipes.Nutrition {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	n := &recipes.Nutrition{
		Calories:     number(m["calories"]),
		Protein:      number(m["proteinContent"]),
		Fat:          number(m["fatContent"]),
		Carbs:        number(m["carbohydrateContent"]),
		Fiber:        number(m["fiberContent"]),
		Sugar:        number(m["sugarContent"]),
		Sodium:       number(m["sodiumContent"]),
		SaturatedFat: number(m["saturatedFatContent"]),
	}
	// Sodium given in grams is converted to milligrams.
	if s := strings.ToLower(strings.ReplaceAll(str(m["sodiumContent"]), " ", "")); strings.HasSuffix(s, "g") && !strings.HasSuffix(s, "mg") {
		n.Sodium *= 1000
	}
	if *n == (recipes.Nutrition{}) {
		return nil
	}
	return n
}

var reNumber = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

func number(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		m := reNumber.FindString(t)
		f, _ := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
		return f
	}
	return 0
}

var reISODuration = regexp.MustCompile(`(?i)^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// ParseDuration converts an ISO 8601 duration ("PT1H30M") to minutes.
func ParseDuration(s string) int {
	m := reISODuration.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	at := func(i int) int { n, _ := strconv.Atoi(m[i]); return n }
	return at(1)*24*60 + at(2)*60 + at(3) + (at(4)+59)/60
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		if len(t) > 0 {
			return str(t[0])
		}
	case map[string]any:
		return str(t["@value"])
	}
	return ""
}

func firstStr(v any) string {
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s := str(x); s != "" {
				return s
			}
		}
	}
	return str(v)
}

func strs(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if s := str(x); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

var reTags = regexp.MustCompile(`<[^>]*>`)

// clean strips tags and entities and collapses whitespace.
func clean(s string) string {
	s = reTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func cleanAll(in []string) []string {
	var out []string
	for _, s := range in {
		if s = clean(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
