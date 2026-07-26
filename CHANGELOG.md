# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]
### Changed
- Serving scaler is plural-aware for composed lines ("1 clove" → "2 cloves", ru "5 зубчиков") via `Intl.PluralRules`, and scales `1/2`, `1 1/2` and `2-3` in hand-written lines.
- Shopping list actions (bought, have it, delete, clear) swap the list in place over HTMX; without JavaScript they still post and redirect.
- German, Spanish and French UI translations are now complete (previously partial with English fallback); Settings no longer labels them "(partial)".

### Fixed
- Serving scaler kept no space between the scaled quantity and its unit ("500г" instead of "500 г").
- Unit labels always used the plural "other" form ("1 cloves", Russian "5 зубчика"): float counts are now formatted for CLDR plural rules.
- Shopping list header and rating labels printed a literal `{Count}`.
- Admin recipe editor: the "Cache image" and "Draft translation" forms were nested inside the main form, so browsers turned them into a Save and detached the translations and the Save button; they now live outside and are wired with `form=` attributes.
- Image storage was always disabled under `docker compose`: the endpoint `http://minio:9000` was passed to minio-go, which only accepts `host:port`. Endpoints now accept an `http(s)://` prefix, and an endpoint that is not up yet is re-probed in the background instead of disabling storage until restart.
- LibreTranslate started (and downloaded its models) on every `docker compose up`; it is now behind `--profile translate`, the app receives `COOK_TRANSLATE_URL`, and the healthcheck no longer relies on curl.
- Machine-translation drafts failed against a real LibreTranslate: batched requests return `{"translatedText": [...]}`, not an array of objects. Both shapes (and API error messages) are handled now; drafting over an existing translation asks for confirmation first.
- Browse favorites: the tab was a link outside the filter form, so any filter change dropped it; it is now a "Favorites" checkbox in the form (hidden for guests).
- Star rating input only highlighted the picked star; now every star up to it lights up, with a hover preview.
- Recipe page showed the same flash message twice after a non-HTMX action.
- Dataset import accepted any `file` name joined onto the inbox path (`../…`); only listed inbox files are allowed now. A run that adds nothing says so instead of "imported". "Cache all images" is hidden while storage is off, and cooked-log notes are capped at 500 characters server-side.

## [1.1.0] - 2026-11-07
### Added
- Grocery shops with search URL templates (RU/US/GB/DE presets, `/admin/shops` CRUD): every ingredient line gets a "Buy" link — the exact product page when the catalog has one, otherwise a search in the user's shop with the localized ingredient name.
- Products gain a shop and a direct product URL (admin form, CSV/JSON import, scanner targets `<CC> <SHOP> <URL>`); dedup keys are namespaced per shop.
- Settings: shop picker and "strict shop" availability (my shop + generic products only).
- One-click recipe dataset import: JSON/JSONL streams of schema.org Recipe objects (OpenRecipes-shaped items included) from the inbox folder or a URL, with a per-run limit and targeted approve; `recipes-import` CLI and `make import-recipes`.
- Seed dataset grew to 46 bilingual EN/RU recipes across 14 cuisines.
- S3-compatible image storage (MinIO in compose by default, graceful degradation without it): `/media` proxy with immutable caching, per-recipe and bulk mirror actions, automatic mirroring after dataset imports; recipe cards and pages show images, imageless cards get per-cuisine gradient placeholders.
- Shopping list page: add from a recipe or the whole meal plan, bought toggles, per-line buy links, "have it" moves items into the pantry, clear bought.
- Recipe favorites with a browse tab; 1–5 star ratings and notes on "I cooked this" shown in the history.
- Serving scaler on the recipe page (client-side, unicode fractions included).
- Optional self-hosted LibreTranslate (`--profile translate`) drafts recipe translations for review.

### Changed
- Migration 00002 (shops, product shop/URL, user shop settings) and 00003 (favorites, shopping list, cooked-log rating/note); `jsonld_targets.txt` documents a vetting checklist and lists four verified live pages.

## [1.0.0] - 2026-11-07
### Added
- README quickstart, `docs/pipelines.md` (CI gates, refresh schedule, review SLAs), admin dashboard summary of pending reviews.

### Changed
- Panics render the localized error page; request bodies are capped; CI pins the linter version.

## [0.7.0] - 2026-11-07
### Added
- Learning path page (5 easy → 5 medium → 5 hard) with progress dots, "try next" suggestions and cooking history.
- "I cooked this" counter and "mark as learned" toggle on recipe pages (HTMX, works without JS).
- Learning progress card on the home page.

## [0.6.0] - 2026-11-07
### Added
- Complete Russian UI (including admin) with correct plural forms; partial German, Spanish and French.
- Language switcher, per-user language setting and `Accept-Language` detection.
- Locale consistency tests and `scripts/add-locale.md`.

## [0.5.0] - 2026-11-07
### Added
- Country-aware recipe list: hides recipes whose essential ingredients aren't sold locally (substitutes allowed), with a "show unavailable" setting.
- Availability badges and substitution notes on the recipe page and in the JSON API.
- Admin product management: list, filters, create/edit, trash/restore, CSV export, CSV/JSON import.
- `internal/catalogscan`: pluggable `CatalogSource` (shop JSON-LD, file inbox, seed fallback) feeding the review queue; product review in `/admin/queue`.
- Settings: "use my location" (offline reverse geocoding) and "re-scan shops".
- CLI: `scan`, `products import|export`; JSON catalog schema and example.

### Fixed
- Ingredient matching now ignores word order; farmer's cheese can be replaced by ricotta.

## [0.4.0] - 2026-11-07
### Added
- Recipe ingestion: `RecipeSource` interface, schema.org JSON-LD source, polite fetcher (robots.txt, per-host rate limit, disk cache, clear User-Agent).
- Normalizer with ingredient parsing, cuisine/meal/difficulty guesses; dedup by canonical URL or content key; user edits are never overwritten.
- Admin review queue with one-click approve/reject, approve all, purge pending and "scrape now".
- `scrape` CLI command (`make scrape-recipes SOURCE=jsonld`), bundled sample pages for offline runs, `docs/ingestion.md`.

## [0.3.0] - 2026-11-07
### Added
- Nutrition calculator (Mifflin–St Jeor + TDEE) with five goals, safety floors and macro targets.
- Daily plan of 3–5 real recipes within ±10% of targets, rotating daily, with an "another idea" button.
- Goals page (metric or imperial input) and a "today's plan" card on the home page.
- Unit and golden tests for targets and plans.

## [0.2.1] - 2026-11-07
### Added
- 23 more bilingual recipes: 28 total across 14 cuisines and all meal times (12 easy, 10 medium, 6 hard).
- Browse page with cuisine, meal, difficulty, text, "only what I can cook" and "hide disliked" filters (HTMX, works without JS).
- Recipe page with video, nutrition card (basic and advanced), ingredient list and step checklist.
- Settings: country and city, language, metric/imperial units, allergies, dislikes and pantry.
- Availability engine with substitutions, recipe pool and read-only JSON API.
- Optional multi-user mode with login and registration.

## [0.2.0] - 2026-11-07
### Added
- SQLite schema with goose migrations: recipes, ingredients, translations, append-only revisions, countries, cities, products, users.
- Ingredient catalogue with EN/RU/DE names, aliases and substitutes; free-text ingredient parser and matcher.
- Recipe store with full CRUD, soft-delete/restore, review status, revision history (last 20 kept) and dedup keys.
- Seed dataset: 14 cuisines, 4 countries with cities, ~150 products per country, 5 bilingual recipes.
- Admin UI: recipe list with filters, create/edit form, trash and restore, revision diff view; basic-auth or loopback guard.
- CLI: `migrate`, `seed`, `recipe create|delete|restore|list`, `approve-all`, `purge-pending`, `backup`.

## [0.1.0] - 2026-11-07
### Added
- Project scaffold: Go module, `cookcompass` binary with `serve`, `healthcheck`, `version`.
- `/healthz` endpoint, config loader (YAML + env), structured logging.
- Makefile, multi-stage Dockerfile, docker-compose with optional Caddy TLS profile.
- Git hooks (`make hooks`) and a CI workflow mirroring `make ci`.
