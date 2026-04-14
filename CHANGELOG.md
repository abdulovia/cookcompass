# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
