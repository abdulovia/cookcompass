package recipes

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsedIngredient is the structured form of a free-text ingredient line.
type ParsedIngredient struct {
	Quantity    *float64 `json:"quantity"`
	Unit        string   `json:"unit"`
	Name        string   `json:"name"`
	Note        string   `json:"note"`
	NeedsReview bool     `json:"needs_review"`
}

// unitAliases maps spellings to canonical units.
var unitAliases = map[string]string{
	"g": "g", "gr": "g", "gram": "g", "grams": "g", "gramme": "g", "grammes": "g", "г": "g", "гр": "g", "грамм": "g",
	"kg": "kg", "kilo": "kg", "kilos": "kg", "kilogram": "kg", "kilograms": "kg", "кг": "kg",
	"mg": "mg", "milligram": "mg", "milligrams": "mg", "мг": "mg",
	"ml": "ml", "milliliter": "ml", "milliliters": "ml", "millilitre": "ml", "millilitres": "ml", "мл": "ml",
	"l": "l", "liter": "l", "liters": "l", "litre": "l", "litres": "l", "л": "l",
	"tsp": "tsp", "tsps": "tsp", "teaspoon": "tsp", "teaspoons": "tsp", "t": "tsp", "ч.л": "tsp", "ч л": "tsp",
	"tbsp": "tbsp", "tbsps": "tbsp", "tbs": "tbsp", "tablespoon": "tbsp", "tablespoons": "tbsp", "ст.л": "tbsp", "ст л": "tbsp",
	"cup": "cup", "cups": "cup", "c": "cup", "стакан": "cup", "стакана": "cup", "стаканов": "cup",
	"oz": "oz", "ounce": "oz", "ounces": "oz",
	"fl oz": "fl oz", "floz": "fl oz",
	"lb": "lb", "lbs": "lb", "pound": "lb", "pounds": "lb",
	"pinch": "pinch", "pinches": "pinch", "щепотка": "pinch", "щепотки": "pinch",
	"dash": "dash", "dashes": "dash",
	"clove": "clove", "cloves": "clove", "зубчик": "clove", "зубчика": "clove", "зубчиков": "clove",
	"can": "can", "cans": "can", "tin": "can", "tins": "can", "банка": "can", "банки": "can",
	"jar": "jar", "jars": "jar",
	"pack": "pack", "packs": "pack", "package": "pack", "packages": "pack", "packet": "pack", "packets": "pack", "упаковка": "pack",
	"slice": "slice", "slices": "slice", "ломтик": "slice", "ломтика": "slice",
	"bunch": "bunch", "bunches": "bunch", "пучок": "bunch",
	"handful": "handful", "handfuls": "handful", "горсть": "handful",
	"sprig": "sprig", "sprigs": "sprig", "веточка": "sprig",
	"stick": "stick", "sticks": "stick",
	"piece": "piece", "pieces": "piece", "pc": "piece", "pcs": "piece", "шт": "piece",
	"head": "head", "heads": "head",
	"stalk": "stalk", "stalks": "stalk",
	"sheet": "sheet", "sheets": "sheet",
}

// Units lists canonical units (for forms and validation).
var Units = []string{"g", "kg", "mg", "ml", "l", "tsp", "tbsp", "cup", "oz", "fl oz", "lb", "pinch", "dash",
	"clove", "can", "jar", "pack", "slice", "bunch", "handful", "sprig", "stick", "piece", "head", "stalk", "sheet", "to taste"}

var unicodeFractions = map[rune]string{
	'½': "1/2", '⅓': "1/3", '⅔': "2/3", '¼': "1/4", '¾': "3/4", '⅕': "1/5",
	'⅖': "2/5", '⅗': "3/5", '⅘': "4/5", '⅙': "1/6", '⅚': "5/6", '⅛': "1/8", '⅜': "3/8", '⅝': "5/8", '⅞': "7/8",
}

var (
	reNumUnit  = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)([a-zA-Zа-яА-Я]+\.?)$`)
	reNumber   = regexp.MustCompile(`^\d+(?:[.,]\d+)?$`)
	reFraction = regexp.MustCompile(`^(\d+)/(\d+)$`)
	reRange    = regexp.MustCompile(`^(\d+(?:[.,]\d+)?(?:/\d+)?)\s*(?:-|–|—|to|до)\s*(\d+(?:[.,]\d+)?(?:/\d+)?)$`)
	reParen    = regexp.MustCompile(`\(([^)]*)\)`)
	reSpaces   = regexp.MustCompile(`\s+`)
)

var toTaste = []string{"to taste", "as needed", "for serving", "for garnish", "по вкусу", "для подачи"}

// ParseIngredient splits "2 1/2 cups flour, sifted" into quantity, unit,
// name and note. When no quantity can be read, NeedsReview is set unless
// the line is explicitly "to taste".
func ParseIngredient(line string) ParsedIngredient {
	s := strings.TrimSpace(line)
	s = strings.TrimLeft(s, "-•*▢□ \t")
	var b strings.Builder
	for _, r := range s {
		if f, ok := unicodeFractions[r]; ok {
			b.WriteString(" " + f + " ")
			continue
		}
		b.WriteRune(r)
	}
	s = reSpaces.ReplaceAllString(strings.TrimSpace(b.String()), " ")

	var notes []string
	// Parenthesized text becomes part of the note, e.g. "(14 oz)".
	s = reParen.ReplaceAllStringFunc(s, func(m string) string {
		if inner := strings.TrimSpace(m[1 : len(m)-1]); inner != "" {
			notes = append(notes, inner)
		}
		return " "
	})
	if i := noteComma(s); i >= 0 {
		if n := strings.TrimSpace(s[i+1:]); n != "" {
			notes = append(notes, n)
		}
		s = s[:i]
	}
	s = reSpaces.ReplaceAllString(strings.TrimSpace(s), " ")

	tasteHit := false
	for _, t := range toTaste {
		lower := strings.ToLower(s)
		if i := strings.Index(lower, t); i >= 0 && len(lower) == len(s) {
			tasteHit = true
			s = strings.TrimSpace(s[:i] + s[i+len(t):])
			notes = append(notes, t)
		}
		for _, n := range notes {
			if strings.Contains(strings.ToLower(n), t) {
				tasteHit = true
			}
		}
	}

	tokens := splitNumUnit(strings.Fields(s))
	p := ParsedIngredient{}
	qty, used := parseQuantity(tokens)
	if used > 0 {
		p.Quantity = &qty
		tokens = tokens[used:]
	} else if len(tokens) > 1 && (strings.EqualFold(tokens[0], "a") || strings.EqualFold(tokens[0], "an")) {
		if _, n := lookupUnit(tokens[1:]); n > 0 {
			one := 1.0
			p.Quantity = &one
			tokens = tokens[1:]
		}
	}
	if unit, n := lookupUnit(tokens); n > 0 && (p.Quantity != nil || len(tokens) > n) {
		p.Unit = unit
		tokens = tokens[n:]
	}
	if len(tokens) > 0 && (strings.EqualFold(tokens[0], "of") || tokens[0] == "x") {
		tokens = tokens[1:]
	}
	p.Name = strings.TrimSpace(strings.Trim(strings.Join(tokens, " "), ".;:"))
	p.Note = strings.Join(notes, "; ")
	switch {
	case p.Name == "":
		p.NeedsReview = true
	case p.Quantity == nil && tasteHit:
		if p.Unit == "" && isToTaste(p.Note) {
			p.Unit = "to taste"
		}
	case p.Quantity == nil:
		p.NeedsReview = true
	}
	return p
}

func isToTaste(note string) bool {
	n := strings.ToLower(note)
	return strings.Contains(n, "to taste") || strings.Contains(n, "as needed") || strings.Contains(n, "по вкусу")
}

// noteComma returns the index of the first comma that is not a decimal
// separator ("0,5"), or -1.
func noteComma(s string) int {
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }
	for i := 0; i < len(s); i++ {
		if s[i] != ',' {
			continue
		}
		if i > 0 && i+1 < len(s) && isDigit(s[i-1]) && isDigit(s[i+1]) {
			continue
		}
		return i
	}
	return -1
}

// splitNumUnit turns a leading "200g" token into "200" "g".
func splitNumUnit(tokens []string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	m := reNumUnit.FindStringSubmatch(tokens[0])
	if m == nil {
		return tokens
	}
	if _, ok := unitAliases[strings.TrimSuffix(strings.ToLower(m[2]), ".")]; !ok {
		return tokens
	}
	return append([]string{m[1], m[2]}, tokens[1:]...)
}

// parseQuantity reads a leading quantity ("2", "1/2", "2 1/2", "1-2",
// "1 to 2") and returns it with the number of tokens consumed. Ranges
// resolve to their lower bound.
func parseQuantity(tokens []string) (float64, int) {
	if len(tokens) == 0 {
		return 0, 0
	}
	if len(tokens) >= 3 {
		if m := reRange.FindStringSubmatch(tokens[0] + " " + tokens[1] + " " + tokens[2]); m != nil {
			if v, ok := number(m[1]); ok {
				return v, 3
			}
		}
	}
	if m := reRange.FindStringSubmatch(tokens[0]); m != nil {
		if v, ok := number(m[1]); ok {
			return v, 1
		}
	}
	v, ok := number(tokens[0])
	if !ok {
		return 0, 0
	}
	used := 1
	if len(tokens) > 1 && reFraction.MatchString(tokens[1]) && !strings.Contains(tokens[0], "/") {
		if f, ok := number(tokens[1]); ok {
			v += f
			used = 2
		}
	}
	return v, used
}

func number(s string) (float64, bool) {
	s = strings.ReplaceAll(s, ",", ".")
	if m := reFraction.FindStringSubmatch(s); m != nil {
		a, _ := strconv.ParseFloat(m[1], 64)
		b, _ := strconv.ParseFloat(m[2], 64)
		if b == 0 {
			return 0, false
		}
		return a / b, true
	}
	if !reNumber.MatchString(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// lookupUnit matches one- or two-token units ("fl oz", "ст. л.").
func lookupUnit(tokens []string) (string, int) {
	if len(tokens) == 0 {
		return "", 0
	}
	// Capital "T" is the conventional abbreviation for tablespoon.
	if tokens[0] == "T" || tokens[0] == "T." {
		return "tbsp", 1
	}
	clean := func(s string) string { return strings.TrimSuffix(strings.ToLower(s), ".") }
	if len(tokens) >= 2 {
		if u, ok := unitAliases[clean(tokens[0])+" "+clean(tokens[1])]; ok {
			return u, 2
		}
		if u, ok := unitAliases[clean(tokens[0])+"."+clean(tokens[1])]; ok {
			return u, 2
		}
	}
	if u, ok := unitAliases[clean(tokens[0])]; ok {
		// A lone "c" or "t" is only a unit when followed by more words.
		if (u == "cup" || u == "tsp") && len(clean(tokens[0])) == 1 && len(tokens) == 1 {
			return "", 0
		}
		return u, 1
	}
	return "", 0
}
