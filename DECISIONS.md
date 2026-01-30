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
