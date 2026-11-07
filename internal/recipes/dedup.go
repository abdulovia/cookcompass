package recipes

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/abdulovia/cookcompass/internal/ingredients"
)

// ContentKey is the dedup key used when no canonical URL is known:
// normalized name plus a hash of the sorted, normalized ingredient names.
// Quantities and ordering do not affect the key.
func ContentKey(name string, ingredientNames []string) string {
	keys := make([]string, 0, len(ingredientNames))
	for _, n := range ingredientNames {
		// Accept raw lines too: quantities and units never affect the key.
		if p := ParseIngredient(n); p.Name != "" {
			n = p.Name
		}
		if k := strings.Join(ingredients.Tokens(n), " "); k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return ingredients.Normalize(name) + "|" + hex.EncodeToString(sum[:8])
}

// RecipeContentKey computes ContentKey for r.
func RecipeContentKey(r *Recipe) string {
	names := make([]string, len(r.Ingredients))
	for i, in := range r.Ingredients {
		names[i] = in.Name
	}
	return ContentKey(r.Name, names)
}

// trackingParams are dropped from canonical URLs.
var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "ref": true, "mc_cid": true, "mc_eid": true}

// CanonicalizeURL lowercases scheme and host, strips "www.", fragments,
// tracking parameters and trailing slashes. Invalid URLs yield "".
func CanonicalizeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Scheme = "https"
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Fragment = ""
	u.User = nil
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(k, "utm_") || trackingParams[k] {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

// Domain returns the host of raw without "www.".
func Domain(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i",
	'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t",
	'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "",
	'э': "e", 'ю': "yu", 'я': "ya",
}

// Slugify makes a URL-safe ASCII slug from name (Cyrillic is transliterated).
func Slugify(name string) string {
	var b strings.Builder
	dash := true
	for _, r := range ingredients.Normalize(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case unicode.Is(unicode.Cyrillic, r):
			if t := translit[r]; t != "" {
				b.WriteString(t)
				dash = false
			}
		default:
			if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 80 {
		s = strings.TrimRight(s[:80], "-")
	}
	if s == "" {
		s = "recipe"
	}
	return s
}
