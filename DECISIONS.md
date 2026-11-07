# Decisions

One line per decision that the requirements left open. Newest at the bottom of each section.

## Platform
- Go 1.24 with `GOTOOLCHAIN=local`; dependencies pinned to versions that support it.
- Router: `go-chi/chi` (stdlib-compatible handlers, small, well known).
- SQLite driver: `modernc.org/sqlite` (pure Go) so the binary is static and the image can be distroless.
- One binary (`cookcompass`) for the server and every maintenance command; the Makefile calls it, `RUN=` switches to the container.
- Config: optional `config.yaml` + `COOK_*` env overrides; env always wins.
- Logging: `log/slog`, JSON by default, text via `COOK_LOG_FORMAT=text`.
- HTMX 1.9 and Alpine 3.14 are vendored into `web/static/vendor` so nothing is fetched from a CDN at runtime.
- Container: distroless `static-debian12:nonroot`; healthcheck uses `cookcompass healthcheck` because the image has no shell or curl.
- Compose binds to `127.0.0.1` only; Caddy (local TLS) is opt-in via the `tls` profile.
- `make backup` uses `VACUUM INTO` (consistent snapshot without stopping the app); in Docker it copies the file out with `docker compose cp`.
- golangci-lint is pinned (v1.64.8) and installed into `bin/` by `make lint`, so the linter always matches the Go toolchain.

## Data model
- One initial goose migration holds the whole schema; later changes get new numbered files.
- `recipe_revisions` is append-only, enforced by a trigger that rejects UPDATE; pruning past `keep_revisions` is the only DELETE.
- Revisions also record soft-delete, restore and status changes, so the diff view shows the full lifecycle.
- `recipe_i18n` also stores `description` and `ingredients_json` (localized display lines aligned by position) so Russian ingredient lists read naturally.
- Ingredients live in their own package and table with aliases in every locale; recipe and product rows link to them by id.
- Unmatched ingredient names are kept without an ingredient id and count as "not available" until mapped.
- Slugs are assigned once and stay stable when a recipe is renamed (links don't break).
- Soft-delete applies to published recipes; `purge-pending` physically removes never-approved queue items, since no user history can reference them.

## Parsing
- The ingredient-line parser ships with the CRUD core (not the scraper phase) because the admin form uses it too.
- Quantity ranges ("1-2", "1 to 2") resolve to the lower bound; the original text is kept in `raw`.
- "to taste" / "as needed" / "по вкусу" lines are valid without a quantity; anything else without one is flagged `needs_review`.
- A comma starts the note unless it sits between digits ("0,5 l"); a capital `T` means tablespoon, lowercase `t` teaspoon.

## Seed data
- Countries: US, GB, DE, RU (4 of the 3–5 requested), each with 5–6 cities for offline reverse geocoding.
- Product catalogs are CSV per country (`seed/products/<cc>.csv`, ~150 SKUs each); prices are typical-shelf approximations in local currency, sizes metric everywhere.
- Each country deliberately lacks a few items (RU: fish sauce, gochujang, dumpling wrappers…; DE: gochujang, gyoza wrappers…) so availability and substitutions are visible out of the box.
- Seed video links point at YouTube search results for the dish; admins can paste a specific video URL, which is then embedded via youtube-nocookie.com.
- The seeder is idempotent and runs on every server start: it upserts ingredients, cuisines and countries, but never overwrites existing recipes or products.

## Web & admin
- UI strings go through `go-i18n` from the first template (English only until the i18n phase) so the translation pass is data, not a rewrite.
- CSRF defence is a same-origin check on Origin/Referer for every non-GET request plus `SameSite=Lax` cookies — no tokens to thread through HTMX.
- Admin guard: basic auth when `COOK_ADMIN_PASSWORD` is set, otherwise loopback clients only. `X-Forwarded-For` is ignored so the loopback check can't be spoofed.
- Compose sets a default admin password (`cookcompass`) because container traffic never arrives from loopback; override it in `.env`.
- Admin ingredient editing is one line per ingredient parsed by the same parser as the scraper; "(optional)" marks non-essential lines; unchanged lines keep their explicit ingredient links.
- Admin edits flip `source` to `user`, making the recipe authoritative against future scrapes.
- Flash messages travel as a `?flash=<id>` query parameter holding an i18n key (no server-side session state).
- CSP allows `'unsafe-eval'` because Alpine.js evaluates attribute expressions; only the vendored scripts are loaded.
- `serve` migrates and seeds on start, so `docker compose up` on an empty volume gives a usable app.

## Users & browsing
- Single-user mode (default) maps every visitor to the local account `cook`; `COOK_MULTI_USER=true` enables register/login with bcrypt and 30-day cookie sessions; guests can browse but not save.
- Allergies are always excluded from every list; dislikes are hidden by default and can be shown with a browse toggle.
- "Only what I can cook" checks the user's pantry with the same availability engine used for countries; salt, pepper, oil and water are assumed staples.
- The availability engine is pure (no I/O) and shared by pantry and country checks; substitutes that are staples also count.
- Unit preference converts weights and volumes (oz/lb/cups ↔ g/ml) with kitchen-friendly rounding; spoons and pieces are left alone.
- Hand-written translated ingredient lines win over composed ones; otherwise lines are built from the parsed quantity, converted unit and localized ingredient name.
- Browse filters re-render only the results block via HTMX (`HX-Target: results`); the same URL works as a normal page without JavaScript.
- A read-only JSON API (`/api/recipes`, `/api/recipes/{slug}`) exposes the same filtered pool; its output is covered by golden files.
- Step checklists persist progress per recipe in the browser's localStorage (nothing is sent to the server).

## Nutrition
- Energy: Mifflin–St Jeor BMR × activity factor (1.2 / 1.375 / 1.55 / 1.725 / 1.9).
- Goals: lose −500 kcal & 1.6 g/kg protein; maintain ±0 & 1.2 g/kg; gain muscle +300 & 2.0 g/kg; gain weight +500 & 1.5 g/kg; eat healthier ±0 & 1.2 g/kg. Fat 30% (25% for muscle gain), carbs fill the rest, fiber 14 g/1000 kcal (min 25 g).
- Safety floors: never below 1200 kcal (female) / 1500 kcal (male); protein capped at 30% of energy. The page says it is not medical advice.
- Daily plan: breakfast + lunch + dinner + 0–2 snacks from the visible pool, exhaustive over combinations, portions scaled in quarter servings (0.5–2.5) with a small penalty for odd portions; ranked by number of macros outside ±10%, then weighted squared error.
- The plan rotates daily via a seed of date + user id; "Another idea" bumps the seed. No plan history is stored.
- Body data is stored metric; imperial users enter ft/in and lb, converted on save.

## Ingestion
- One working source: schema.org `Recipe` JSON-LD. No public JSON-LD recipe site with clearly permissive terms was found, so the default targets are three bundled sample pages (`file:` URLs) and operators add sites they may index.
- `file:` targets (globs, relative to the targets file) make the pipeline work offline and double as "import a saved page".
- robots.txt: specific user-agent group beats `*`; longest match wins, Allow wins ties; 4xx = allow all, 5xx = disallow all; Crawl-delay honoured up to 30 s on top of the 1 s minimum.
- HTTP responses (including robots.txt) are cached on disk by URL hash for `cache_ttl` (7 days).
- A scrape that changes nothing writes no revision (avoids pruning real history away); every applied or withheld change does.
- Re-scrapes keep the review decision (approved stays approved, rejected stays rejected) and keep non-English translations.
- Scraped difficulty, meal times and cuisine are heuristic guesses shown in the queue for the reviewer to correct; unknown cuisines stay empty.
- Only YouTube video URLs and http(s) image URLs are kept; nothing binary is downloaded.
- An offline `scrape` run logs a warning and exits 0 so scripts and cron jobs don't fail when the network is down.

## Catalog & availability
- A country "has" an ingredient when at least one approved, non-deleted product links to it; pending and rejected products never count.
- Recipe availability defaults to 100% of essential, non-staple ingredients (direct or via a declared substitute); optional lines never block. Threshold: `availability.threshold`.
- Unlinked essential ingredients count as missing (the recipe page says so), which keeps unreviewed scraped recipes from surfacing as cookable.
- Unavailable recipes stay reachable by URL and explain what's missing; the browse list hides them unless "show unavailable" is on (off by default).
- Catalog sources run in order and their results merge: shop pages with schema.org `Product` JSON-LD (`seed/sources/catalog_targets.txt`, `<CC> <URL>`), then CSV/JSON files in `catalog_inbox_dir/<CC>/`. If both yield nothing, missing seed products are restored (never overwriting existing rows) and a warning is logged.
- Scanned products land as pending; CSV/JSON imports from the admin are treated as user edits (approved, `source=user`) and are never overwritten by scans.
- Product dedup key: SKU (or GTIN) when present, else the normalized product name, per country.
- Ingredient matching is word-order insensitive ("паста карри зелёная" = "зелёная паста карри") so product names from different shops link correctly.
- Reverse geocoding is offline: nearest bundled city by haversine distance, accepted within 1500 km; otherwise the current country is kept and the user picks manually.
- The seed was missing a farmer's cheese → ricotta substitute; added so syrniki is cookable where only ricotta is sold.
