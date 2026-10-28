# Adding recipes

A fresh install already has 46 approved recipes (EN/RU, 14 cuisines). These are the ways to add your own,
from one recipe to hundreds. Everything that comes from outside goes through the **review queue**
(`/admin/queue`), so nothing appears on the site until you approve it.

| Way | Good for | Where |
|---|---|---|
| Admin form | one recipe typed by hand | *Admin → Recipes → New* |
| JSON file | recipes you keep in git or share | `content/recipes/<slug>.json`, loaded on restart |
| CLI | scripting | `make recipe-create FILE=my.json` |
| TheMealDB | ~300 recipes with photos and videos, one click | *Admin → Queue → Scrape now: themealdb* |
| Recipe pages | specific pages with schema.org markup | `seed/sources/jsonld_targets.txt`, *Scrape now: jsonld* |
| Dataset files | bulk dumps (JSON / JSONL) | `data/recipe-inbox/`, *Admin → Queue → Import recipes* |

## 1. Admin form

*Admin → Recipes → New.* Each ingredient goes on its own line (`200 g spaghetti`, `1 tsp salt`). Lines are parsed into
quantity, unit and name; lines the parser can't read are flagged *needs review*. Translations are in the same form.
Every save creates a revision (*History* shows diffs), and deleting is a soft delete you can undo.

## 2. A JSON file in `content/recipes/`

Copy [`content/examples/recipes/buttered-toast.json`](../content/examples/recipes/buttered-toast.json) to
`content/recipes/my-dish.json`, edit it, and restart (`docker compose restart app` or `make run`). The file name is the
URL slug (`/recipes/my-dish`). The file is loaded once; later edits happen in the admin, so your changes there are never
overwritten. If a file has a mistake, it is skipped, and the admin checklist and `make doctor` name it.

| Field | Values |
|---|---|
| `cuisine` | slug from `seed/cuisines.json` (`italian`, `russian`, …) or one you add in `content/cuisines.json` |
| `meal_times` | any of `breakfast`, `lunch`, `dinner`, `snack` |
| `difficulty` | `easy`, `medium`, `hard` (the learning path climbs 5 → 5 → 5) |
| `prep_minutes`, `cook_minutes`, `servings` | numbers |
| `nutrition` | per serving: `calories`, `protein`, `fat`, `carbs`, `fiber`, `sugar`, `sodium` (mg), `saturated_fat`; recipes without calories are left out of the meal plan |
| `ingredients[]` | `text` (the line as shown), optional `ingredient` (slug from `seed/ingredients.json`, which turns on availability, substitutions and buy links), optional `"essential": false` |
| `video_url`, `image_url` | a YouTube link (embedded) and an image URL |
| `i18n.<lang>` | `name`, `description`, `steps[]`, and optionally `ingredients[]` (hand-written lines in that language) |

Unknown ingredient slugs make the file fail. Leave `ingredient` out if there's no matching slug, or add the ingredient
to `content/ingredients.json` (same format as `seed/ingredients.json`, with names per language).

## 3. TheMealDB (built in)

*Admin → Queue → Scrape now: themealdb*, or `make scrape-recipes SOURCE=themealdb`. It runs in the background
(~30 s, one request per second) and queues around 300 recipes with photos and YouTube videos. Then review them in the queue:
fix the cuisine or difficulty if the guess is off, add nutrition if you want them in the meal plan, and approve.
Running it again only adds what is new.

TheMealDB's public test key `1` is meant for development and personal or educational use. For anything public,
become a supporter there and set `COOK_MEALDB_KEY`. Recipes keep a link to their TheMealDB page.

## 4. Other sources

- **Recipe pages** with schema.org `Recipe` JSON-LD: add URLs to `seed/sources/jsonld_targets.txt` (it starts with a
  vetting checklist) and *Scrape now: jsonld*. robots.txt is obeyed and requests are rate-limited.
- **Dataset files**: drop `.json`/`.jsonl` files with schema.org Recipe objects (or OpenRecipes-shaped records) into
  `data/recipe-inbox/` and use *Import recipes from a dataset* in the queue. See [ingestion.md](ingestion.md).

## Where to get recipes, and whether you may

Only import what you are allowed to use. Typical options:

| Source | What | Terms in short |
|---|---|---|
| Your own recipes | the best ones | yours |
| [TheMealDB](https://www.themealdb.com/api.php) | ~300 curated recipes, photos, videos | free test key for personal/educational use; supporters for production |
| [Wikibooks Cookbook](https://en.wikibooks.org/wiki/Cookbook:Recipes) | thousands of community recipes | CC BY-SA: credit the source and share alike |
| [OpenRecipes](https://github.com/fictivekin/openrecipes) dumps | large JSON dumps of recipe metadata | links to the original sites; recipe text belongs to them, so use it for private indexing only |
| Recipe sites with JSON-LD | individual pages | check each site's terms and robots.txt first; many forbid copying |

Ingredient lists and plain cooking facts are generally not protected; written instructions, photos and videos usually
are. When in doubt, keep imports to your own private installation and link back to the source (CookCompass keeps
`source_url`/`canonical_url` on every imported recipe).
