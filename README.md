# CookCompass

A self-hosted web app that teaches you to cook — and only suggests recipes whose ingredients are sold where you live.
28 bilingual (EN/RU) beginner-friendly recipes from 14 cuisines, a daily meal plan for your nutrition goal,
a learning path from easy to hard, and a local catalog of what shops in your country sell. Runs on your laptop; no cloud, no tracking.

## Quickstart

Requirements: Docker with Compose v2 (for `make up`) or Go 1.24 (for running without Docker), and `make`.

```sh
git clone https://github.com/abdulovia/cookcompass.git && cd cookcompass
make up                 # build the image, start it, wait for /healthz
open http://localhost:8080
```

1. **Settings** → pick your country (or "Use my location"), language and units; tick allergies, dislikes and what's in your pantry.
2. **Goals** → enter height, weight, age, activity and a goal → get targets and today's 3–5 recipe plan.
3. **Recipes** → filter by cuisine, meal, difficulty or "only what I can cook"; open a recipe, follow the step checklist, press *I cooked this*.
4. **Learn** → mark recipes as learned to climb 5 easy → 5 medium → 5 hard.
5. **Admin** → http://localhost:8080/admin (user `admin`, password `cookcompass` — change it in `.env`, see `.env.example`).

Stop with `make down`. Data lives in the `cookdata` Docker volume; `make backup` writes a snapshot to `backups/`.

Without Docker: `make run` (serves on :8080, data in `./data/`). Configuration: copy `config.example.yaml` to
`config.yaml` or set `COOK_*` environment variables.

## Everyday commands

| Command | What it does |
|---|---|
| `make test` | all tests with race detector and coverage gate (≥ 70% of `internal/`) |
| `make ci` | format check, vet, lint, tests, build, Docker image |
| `make scrape-recipes SOURCE=jsonld` | scrape recipe pages into the review queue (bundled samples work offline) |
| `make scan COUNTRY=RU` | refresh product catalogs (shop pages, inbox files, seed fallback) |
| `make approve-all` / `make purge-pending` | accept or drop everything waiting for review |
| `make recipe-create FILE=docs/examples/recipe.json` / `make recipe-delete ID=7` | recipe CRUD from the shell (soft delete) |
| `make import-products FILE=x.csv COUNTRY=DE` / `make export-products COUNTRY=DE` | catalog bulk edit as CSV |
| `make seed` / `make migrate` / `make backup` / `make hooks` | maintenance |

CLI targets use the local binary; add `RUN="docker compose exec -T app /app/cookcompass"` to run them in the container.
Optional local TLS via Caddy: `docker compose --profile tls up -d` → https://localhost:8443.

## Documentation

- [DECISIONS.md](DECISIONS.md) — every choice the requirements left open, one line each
- [CHANGELOG.md](CHANGELOG.md) — what changed per release
- [docs/ingestion.md](docs/ingestion.md) — scraping pipeline and how to add sources
- [docs/pipelines.md](docs/pipelines.md) — CI gates, refresh schedule, review SLAs
- [scripts/add-locale.md](scripts/add-locale.md) — adding a UI language
- [docs/schemas/catalog.schema.json](docs/schemas/catalog.schema.json) — offline/in-store catalog format

Layout: `cmd/cookcompass` (single binary), `internal/{recipes,ingredients,catalog,catalogscan,ingest,fetch,availability,pool,nutrition,learning,users,i18n,admin,site,server,cli,…}`,
`web/` (templates, static assets, locales — embedded), `seed/` (bundled dataset), `migrations/`.
