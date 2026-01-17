// Package ingredients manages canonical ingredients, their aliases,
// translations and substitutes, and matches free-text names to them.
package ingredients

import (
	"strings"
	"unicode"
)

// Normalize lowercases s, folds ё→е, turns punctuation into spaces and
// collapses whitespace.
func Normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r == 'ё':
			r = 'е'
		case r == '’' || r == '\'':
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '%' {
			b.WriteRune(foldAccent(r))
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// foldAccent maps common Latin accented letters to ASCII.
func foldAccent(r rune) rune {
	switch r {
	case 'á', 'à', 'â', 'ä', 'ã', 'å':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'ö', 'õ':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ñ':
		return 'n'
	case 'ç':
		return 'c'
	case 'ß':
		return 's'
	}
	return r
}

// Singular reduces a simple English plural to its singular form.
func Singular(w string) string {
	n := len(w)
	switch {
	case n <= 3 || !isASCII(w):
		return w
	case strings.HasSuffix(w, "ss") || strings.HasSuffix(w, "us") || strings.HasSuffix(w, "is"):
		return w
	case strings.HasSuffix(w, "ies"):
		return w[:n-3] + "y"
	case strings.HasSuffix(w, "oes"), strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"), strings.HasSuffix(w, "xes"):
		return w[:n-2]
	case strings.HasSuffix(w, "ves") && n > 4:
		return w[:n-3] + "f"
	case strings.HasSuffix(w, "s"):
		return w[:n-1]
	}
	return w
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Key normalizes s and singularizes each word, giving a stable lookup key.
func Key(s string) string {
	words := strings.Fields(Normalize(s))
	for i, w := range words {
		words[i] = Singular(w)
	}
	return strings.Join(words, " ")
}

// descriptors are words that describe preparation or size rather than
// identity; they are ignored when matching.
var descriptors = map[string]bool{
	"fresh": true, "freshly": true, "chopped": true, "finely": true, "roughly": true, "coarsely": true,
	"minced": true, "diced": true, "sliced": true, "thinly": true, "grated": true, "shredded": true,
	"large": true, "small": true, "medium": true, "big": true, "whole": true, "peeled": true,
	"crushed": true, "beaten": true, "softened": true, "melted": true, "cold": true, "warm": true,
	"room": true, "temperature": true, "organic": true, "boneless": true, "skinless": true,
	"optional": true, "packed": true, "heaping": true, "level": true, "about": true, "of": true,
	"a": true, "an": true, "the": true, "and": true, "or": true, "for": true, "to": true, "taste": true,
	"serving": true, "garnish": true, "plus": true, "more": true, "cut": true, "into": true, "piece": true,
	"cube": true, "drained": true, "rinsed": true, "halved": true, "quartered": true, "trimmed": true,
	"lightly": true, "toasted": true, "frozen": false, "dried": false, "ground": false,
}

// Tokens returns the significant words of a name for matching.
func Tokens(s string) []string {
	var out []string
	for _, w := range strings.Fields(Key(s)) {
		if descriptors[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}
