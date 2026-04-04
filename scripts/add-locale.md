# Adding a UI language

UI strings live in `web/locales/<code>.json` (one flat object of message id → text) and are loaded with
[go-i18n](https://github.com/nicksnyder/go-i18n). English (`en.json`) is the source of truth and the fallback:
any id missing from a locale is shown in English, so a language can ship partially translated.

1. **Create the file.** Copy the ids you want to translate from `web/locales/en.json` into
   `web/locales/<code>.json`, where `<code>` is a BCP 47 tag (`de`, `pt-BR`, …). Always include
   `"meta.language_name"` with the language's own name ("Deutsch") — it's shown in the switcher.
2. **Keep placeholders.** `{{.Count}}`, `{{.Name}}` etc. must appear exactly as in English.
3. **Plurals.** Ids whose English value is an object (`{"one": …, "other": …}`) need the plural forms of
   your language ([CLDR rules](https://www.unicode.org/cldr/charts/latest/supplemental/language_plural_rules.html));
   Russian, for example, uses `one`, `few`, `many` and `other`.
4. **Check.** `go test ./internal/i18n/` fails on unknown ids, mismatched placeholders or a plural/non-plural
   mismatch. A language is shown as "(partial)" in Settings until every English id is translated; once complete,
   update the expectation in `internal/i18n/locales_test.go`.
5. **Recipe content** is translated separately, per recipe, in the admin form (Translations section) or in the
   seed files (`seed/recipes/*.json`, `"i18n": {"<code>": {...}}`). Ingredient names are translated in
   `seed/ingredients.json` (`"names"`).
6. Rebuild (`make build` / `make up`): locale files are embedded in the binary.

Status: `en` and `ru` are complete; `de`, `es` and `fr` contain navigation and common labels only.
