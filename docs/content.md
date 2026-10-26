# Customizing CookCompass with `content/`

CookCompass separates **behaviour** (Go code, templates) from **words and data** (names, texts, countries, shops,
products, recipes, branding). The data ships embedded in the binary (`seed/`, `web/locales/`) so a fresh install works
offline. Anything you put in `content/` is **layered on top** and applied when the app restarts (`docker compose restart app`),
with no rebuild.

```
content/                         (Docker: ./content mounted at /data/content; elsewhere COOK_CONTENT_DIR, default ./content)
├── site.yaml                    branding and feature switches        → refines seed/site.yaml key by key
├── cuisines.json                cuisines with names per language     → merged by "slug"
├── countries.json               countries, cities, map centre        → merged by "code"
├── categories.json              product/ingredient category names    → merged by "slug"
├── ingredients.json             ingredients, aliases, substitutes    → merged by "slug"
├── shops.csv                    shops and their search URL           → rows upsert by country + slug
├── products/<cc>.csv            catalog of what country <cc> sells   → rows upsert by SKU
├── recipes/<slug>.json          extra recipes                        → created once (see recipes.md)
├── locales/<lang>.json          UI text                              → merged by message id; new languages allowed
└── static/…                     files served under /static/…         → replace or add (logo, custom.css)
```

Rules of thumb:

- **List only what you change.** A content `countries.json` with one country adds or replaces that country and keeps the
  bundled ones. A `locales/ru.json` with one key changes that string and keeps the rest.
- **Names live with the data.** Cuisines, countries and categories carry `"names": {"en": …, "ru": …}`; missing
  languages fall back to English, then to a readable form of the slug.
- **Templates are not overridable**, so upgrades never break your pages. Use texts, `site.yaml` and `custom.css` instead.
- Examples for everything are in [`content/examples/`](../content/examples/). Copy them one file at a time.

## Branding and features: `site.yaml`

```yaml
name: {en: Family Kitchen, ru: Семейная кухня}    # or just: name: Family Kitchen
tagline: {en: Cook what our shops sell.}
logo: /static/logo.svg                            # or an emoji: "🍲"  (put logo.svg in content/static/)
accent: "#c0392b"                                 # brand colour; shades are derived
stylesheet: /static/custom.css                    # extra CSS after the bundled one
footer_links:
  - {label: {en: About, ru: О нас}, url: https://example.org/about}
features:            # off = no page, no menu entry, no buttons
  shopping_list: true
  learning: true
  goals: false       # nutrition goals and the daily meal plan
  favorites: true
  geolocation: true  # "Use my location" in Settings
```

Every key and its default is in [`seed/site.yaml`](../seed/site.yaml). The file is validated on start: a bad colour,
an unknown feature or a non-`/static/` path stops the app with a clear message rather than breaking pages.

## Adding a country with its shops and products

1. `content/countries.json`:
   ```json
   [{"code": "FR", "name": "France", "names": {"ru": "Франция", "fr": "France"}, "currency": "EUR",
     "lat": 46.6, "lng": 2.2, "default_locale": "fr",
     "cities": [{"name": "Paris", "lat": 48.8566, "lng": 2.3522}]}]
   ```
2. `content/shops.csv`: `country,slug,name,website,search_url_template`, where `{q}` in the template is replaced by
   the ingredient name for "buy" links:
   ```csv
   country,slug,name,website,search_url_template
   FR,monoprix,Monoprix,https://www.monoprix.fr,https://www.monoprix.fr/search?q={q}
   ```
3. `content/products/fr.csv`: what is sold there. `ingredient` is the slug from `seed/ingredients.json`. Price is
   optional; leave it empty unless you know it.
   ```csv
   sku,name,category,ingredient,price,unit
   FR-BREAD-1,Baguette,bakery,bread,,1 pc
   ```
4. Restart. The country appears in Settings, recipes are checked against its catalog, and buy links point at the shop.

Large catalogs can also be imported as CSV in *Admin → Products* or with `make import-products`; see the
[catalog schema](schemas/catalog.schema.json).

## Changing or adding UI text

`content/locales/en.json` (or any language) with just the keys you want to change:

```json
{"footer.local": "Served from our family kitchen server.", "home.hero_title": "What shall we cook today?"}
```

All keys are in [`web/locales/en.json`](../web/locales/en.json). Keep placeholders like `{{.Count}}` intact; plural
texts are objects (`{"one": …, "other": …}`). A whole new `content/locales/it.json` adds Italian to the language
switcher (see [scripts/add-locale.md](../scripts/add-locale.md)).

## Recipes

See [recipes.md](recipes.md).
