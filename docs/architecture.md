# Architecture

One Go binary serves the web UI (server-rendered `html/template` + HTMX + a little Alpine.js) and runs every
maintenance command. Data lives in one SQLite file. There are no runtime cloud dependencies: external calls (scrapers,
TheMealDB, S3, LibreTranslate) are optional and degrade to bundled data.

## Layers

```
            ┌──────────────── content/ (operator: data, texts, branding) ────────────────┐
            │   layered over the embedded defaults: seed/ (data) and web/ (UI)            │
            └───────────────┬───────────────────────────────┬────────────────────────────┘
                            │ internal/content              │
            seeder (JSON merged by key,           Labels (names of cuisines/countries/categories)
            CSV rows, recipes)                    Site (site.yaml: branding, features)
                            │                     i18n bundle (locale files merged by id)
                            ▼                               ▼
   SQLite ◄── stores: recipes, ingredients, catalog, shops, users       web.Page: .T text · .L data names
                            ▲                                        .E/.Options enums · .Nav · .Site
                            │                                               ▼
   ingest (jsonld, dataset, themealdb) ──► review queue        templates (web/templates, embedded)
   catalogscan (shop pages, inbox files)                                   ▼
                                                              site (public) · admin · JSON API
```

**Behaviour lives in code, words and data live in content.** Templates never spell out option lists or labels. They ask
the page: `.Options "meal" .Selected`, `.E "difficulty" .Value`, `.L "cuisine" .Slug`, `.Nav`, `.Feature "goals"`.
The tables behind those calls (`web.Enums`, `web.PublicNav`/`AdminNav`) are the single source for both the UI and route
mounting, so a feature switched off in `site.yaml` has no page, no menu entry and no buttons.

## Request flow

`server` middleware: request id → logging → recover → security headers (CSP) → body limit → same-origin check for
POSTs → **identify** (profile: default user, session user or guest) → **locale** (cookie → profile → Accept-Language; puts
the localizer and the shared `web.UI`, i.e. labels and site config, into the context) → handler. Handlers build a
`web.Page` and render a page or, for HTMX requests, just the block being swapped (`R.Partial`).

## Packages

| Package | Role |
|---|---|
| `cmd/cookcompass` | entry point; `internal/cli` dispatches `serve`, `init`, `doctor`, `seed`, `scrape`, … |
| `internal/app` | wires config, DB and stores; seeding, scraping, background jobs, setup checks |
| `internal/content` | layered file system, merge-by-key, data labels, `site.yaml` |
| `internal/config` | `config.yaml` + `COOK_*` environment |
| `internal/server` | HTTP server, middleware, static files and `/media` |
| `internal/site`, `internal/admin` | public pages and API; admin CRUD, queue, imports |
| `internal/web` | page model, renderer, enums, navigation |
| `internal/i18n` | go-i18n bundle with CLDR plurals and layered locale files |
| `internal/recipes`, `ingredients`, `catalog`, `shops`, `users` | domain models and SQLite stores |
| `internal/availability`, `pool` | what you can cook where you live, substitutions, filters |
| `internal/nutrition`, `learning` | targets and meal plans (Mifflin–St Jeor), learning path |
| `internal/ingest`, `catalogscan`, `fetch` | recipe and catalog ingestion, polite HTTP client |
| `internal/storage`, `translate` | optional S3 image storage, optional LibreTranslate drafts |
| `migrations/` | goose SQL migrations, applied on start |

## Data rules

- Soft delete only. Every recipe edit or scrape is an append-only revision (the last 20 are kept).
- Provenance (`seed`, `scraped:<domain>`, `user`): user edits always win, and re-imports never silently overwrite them.
- Imported content is `pending` until approved in `/admin/queue`.
- Bundled and content recipes are created once; afterwards the database is the source of truth.
