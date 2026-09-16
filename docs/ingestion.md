# Recipe ingestion

How recipes get from the web (or files) into CookCompass, and how to add new sources.

## Pipeline

```
targets file / --url ──► RecipeSource.Fetch ──► []RawRecipe ──► Importer.Normalize ──► dedup ──► review queue (pending)
                          (polite fetcher)                       (parse, guess)          │            │
                                                                                         │      admin approves
                                                                                         ▼            ▼
                                                                         revision recorded     visible to users
```

| Stage | Code | What it does |
|---|---|---|
| Fetch | `internal/fetch` | Clear User-Agent, robots.txt (longest-match, `*` / `$`, Crawl-delay), ≥ 1 request/second per host, on-disk cache (`scrape.cache_dir`, TTL `scrape.cache_ttl`), 5 MB limit. `file:` URLs read local HTML. |
| Source | `internal/recipes/source.go` (interface), `internal/ingest/ingest.go` (`JSONLDSource`), `internal/ingest/dataset.go` (`DatasetSource`) | `JSONLDSource` extracts schema.org `Recipe` objects from `<script type="application/ld+json">`, including `@graph`, arrays, `HowToSection`/`HowToStep`, ISO 8601 durations, `NutritionInformation`, `VideoObject`. `DatasetSource` reads whole dumps (see below). |
| Normalize | `Importer.Normalize` | Parses every ingredient line into quantity/unit/name (unparseable → `needs_review`), maps cuisine names and aliases, guesses meal times from category/keywords and difficulty from time/steps/ingredients, keeps only YouTube video URLs and http(s) image URLs (never binaries). |
| Dedup | `Importer.Import` | Key = canonical URL (normalized: https, no `www.`, no tracking params, no trailing slash); otherwise normalized name + hash of sorted ingredient names. |
| Store | `recipes.Store` | New → `status=pending`, `source=scraped:<domain>`. Existing scraped/seed recipe → updated in place (status kept). Existing `source=user` → **not applied**; a revision with `applied=0` records what the scrape found. Identical content → nothing written. |
| Review | `/admin/queue` | Approve / reject per recipe (one click, HTMX), approve all, purge pending, "scrape now". Rejected recipes stay rejected on re-scrape. |

## Running it

```sh
make scrape-recipes SOURCE=jsonld           # all targets in seed/sources/jsonld_targets.txt
./bin/cookcompass scrape --url https://example.com/some-recipe --limit 1
make approve-all                            # or review in /admin/queue
make purge-pending                          # drop everything still waiting
```

Inside Docker: `make scrape-recipes RUN="docker compose exec -T app /app/cookcompass"`.

**Offline:** unreachable or blocked targets are logged and skipped. If nothing could be fetched the command
prints a warning and exits 0; the bundled seed recipes are untouched. The default targets file lists a few
vetted live pages only; to try the pipeline offline, save pages from your browser and add `file:` entries.

## Dataset import (bulk loading)

The `dataset` source reads whole recipe dumps: any stream of JSON values — a pretty-printed array, JSONL
with one (possibly multi-line) object per record, or a mix. Objects typed `Recipe` use the schema.org
mapping; untyped items with a name plus an ingredients list (the OpenRecipes dump shape) are accepted.
Everything still passes the completeness gate: no steps or no ingredients → rejected and counted as invalid.

- **Inbox:** drop `*.json` / `*.jsonl` files into `data/recipe-inbox/` (mount it in Docker), then
  Admin → Review queue → *Import recipes from a dataset*, or `make import-recipes FILE=drop.jsonl`.
- **URL:** point the form (or `URL=…`) at an http(s) dataset ≤ 5 MB (the polite fetcher's cap).
- **Limit:** each run takes at most N recipes (default 200) so a 170k-item dump can be sampled before bulk.
- **Approve:** off by default — imports land in the review queue like scrapes. When on, only what this
  run queued is approved; older pending items stay.
- **Images:** when S3 storage is configured and `scrape.storage.s3.cache_images` is true, imported images
  are mirrored into the bucket and served from `/media`.

Licensing is your responsibility: import datasets you may use for personal self-hosted cooking.

## Choosing targets

Edit `seed/sources/jsonld_targets.txt` (or point `COOK_SCRAPE_TARGETS` at your own file):

- one URL per line, `#` for comments;
- `file:` lines read local HTML, relative to the targets file, globs allowed (save a page from your browser and import it);
- only list sites whose terms allow indexing for personal use. robots.txt is enforced regardless.

Before adding a live site, vet it (most big recipe sites now sit behind Cloudflare or ban AI crawlers):

1. `robots.txt` must allow all agents (a Yoast-style `User-agent: * Disallow:` is fine);
2. the page must return 200 to `CookCompassBot` — check with `curl -A CookCompassBot/1.0 <url>`;
3. its JSON-LD `Recipe` must carry both `recipeIngredient` and `recipeInstructions` (some sites ship only
   the former; those records die in the completeness gate).

## Machine translation drafts

With a self-hosted LibreTranslate (`docker compose --profile translate up -d libretranslate`, then
`COOK_TRANSLATE_URL=http://libretranslate:5000` for the app), the admin recipe form grows a
*Draft translation* button per locale. It fills that locale's fields from the English base and records
a "machine translation draft" revision — review and fix the text in the same form before publishing.

## Adding a source

1. Implement `recipes.RecipeSource`:

   ```go
   type MySource struct{ Fetcher *fetch.Client }
   func (s *MySource) Name() string { return "mysource" }
   func (s *MySource) Fetch(ctx context.Context, opts recipes.FetchOpts) ([]recipes.RawRecipe, error) { … }
   ```

   Use `fetch.Client` for every HTTP request so robots.txt, rate limiting and caching apply. Return what you
   could read plus an error only when nothing was readable.
2. Register it in `ingest.Sources` (`internal/ingest/ingest.go`).
3. Add fixture pages under `internal/ingest/testdata/` and a test; tests must never touch the network
   (serve fixtures with `httptest`).
4. `make scrape-recipes SOURCE=mysource`.

## Extensions

Not built yet; each slots in behind `RecipeSource` without touching the importer or the queue.

- `TODO(extension)`: **sitemap source** — walk `sitemap.xml` of an allowed site and feed recipe URLs to the JSON-LD extractor.
- `TODO(extension)`: **microdata / RDFa source** — older sites mark recipes up with `itemprop` instead of JSON-LD.
- `TODO(extension)`: **API sources** — e.g. open recipe APIs; map their JSON onto `RawRecipe`.
- `TODO(extension)`: **bulk file import** — a `RecipeSource` reading a directory of `recipes.Input` JSON files (the format of `seed/recipes/*.json`).
- `TODO(extension)`: **translation assist** — pre-fill `recipe_i18n` for other locales during review.
- `TODO(extension)`: **ingredient mapping UI** — let reviewers link unmatched ingredient lines to canonical ingredients (today: edit the line text or add an alias in `seed/ingredients.json`).
- `TODO(extension)`: **scheduled runs** — see `docs/pipelines.md`.
