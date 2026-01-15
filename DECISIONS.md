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
