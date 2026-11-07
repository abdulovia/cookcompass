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
| Source | `internal/recipes/source.go` (interface), `internal/ingest/ingest.go` (`JSONLDSource`) | Extracts schema.org `Recipe` objects from `<script type="application/ld+json">`, including `@graph`, arrays, `HowToSection`/`HowToStep`, ISO 8601 durations, `NutritionInformation`, `VideoObject`. |
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
prints a warning and exits 0; the bundled seed recipes are untouched. The default targets file points at
three sample pages in `seed/sources/samples/` so the pipeline can be tried without network access.

## Choosing targets

Edit `seed/sources/jsonld_targets.txt` (or point `COOK_SCRAPE_TARGETS` at your own file):

- one URL per line, `#` for comments;
- `file:` lines read local HTML, relative to the targets file, globs allowed (save a page from your browser and import it);
- only list sites whose terms allow indexing for personal use. robots.txt is enforced regardless.

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
