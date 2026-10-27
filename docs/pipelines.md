# Pipelines

How CookCompass is built and checked today, and how data is meant to stay fresh once it runs unattended.
Nothing here needs cloud infrastructure; every job is a `cookcompass` subcommand you can run by hand.

## Build & test (today)

| Gate | Local command | CI job (`.github/workflows/ci.yml`) |
|---|---|---|
| Format | `make fmt-check` | `test` |
| Vet + lint (golangci-lint v1.64.8, pinned) | `make vet lint` | `lint` |
| Tests, race detector, ≥ 70% coverage of `internal/` | `make test` | `test` |
| Binary | `make build` | `build` |
| Container image | `make docker-build` | `docker` |
| All of the above | `make ci` | — |

**Releases.** Pushing a `v*` tag runs `.github/workflows/release.yml`: the tests, then a multi-arch
(linux/amd64, linux/arm64) image pushed to `ghcr.io/abdulovia/cookcompass` as `:X.Y.Z`, `:X.Y` and `:latest`.
`make up-release` runs that image. Make the package public once in the GitHub package settings.

The pre-commit hook (`make hooks`) runs `gofmt`, `go vet` and `make test-short` on every commit.
Tests never touch the network: scrapers are exercised against recorded fixtures served by `httptest`.

## Data refresh (planned schedule)

| Job | Command | Cadence | Notes |
|---|---|---|---|
| Recipe ingestion | `cookcompass scrape --source jsonld` | weekly (Sun 03:00 local) | Polite by construction: robots.txt, ≥ 1 s/host, 7-day HTTP cache, so a weekly run re-fetches each page at most once. |
| Dataset import | `cookcompass recipes-import --file <drop> --approve` | whenever you drop a file | Inbox-driven, not scheduled; repeats are deduplicated, so re-running the same dump is a no-op. |
| Catalog scan | `cookcompass scan --country all` | weekly (Sun 04:00) | Shop pages + inbox files; offline runs fall back to the seed catalog. |
| Backup | `make backup` | daily (02:00) | `VACUUM INTO` snapshot; keep 14 daily + 8 weekly copies. |
| Purge stale queue | `cookcompass purge-pending` | monthly | Only items nobody reviewed within the SLA below. |

`TODO(extension)`: a `schedule` subcommand (or a tiny sidecar container running cron) that runs the jobs above and
writes a summary line per run to the log. Until then, a host crontab works:

```cron
0 2 * * *  cd /path/to/cookcompass && make backup
0 3 * * 0  docker compose exec -T app /app/cookcompass scrape --source jsonld
0 4 * * 0  docker compose exec -T app /app/cookcompass scan --country all
```

## Review SLAs

Scraped recipes and scanned products never reach users without a human decision.

| Item | Target | Why |
|---|---|---|
| New recipe in `/admin/queue` | reviewed within 7 days | Check ingredient lines flagged "needs review", difficulty and meal times; add a Russian translation if possible. |
| New product in `/admin/queue` | reviewed within 7 days | Link unmatched products to an ingredient so availability is correct. |
| Withheld scrape (revision marked "not applied") | glance at weekly | Upstream changed a recipe you edited; merge by hand if the change is useful. |
| Pending item older than 30 days | purged | Keeps the queue meaningful; a later scrape will bring it back if it still exists. |

## Data lifecycle summary

- **Recipes:** seed / scraped / user → pending → approved or rejected → (soft-)deleted ↔ restored; every change is an append-only revision (last 20 kept).
- **Products:** seed / scanned / imported → pending (scans) or approved (seed, admin imports) → soft-deleted ↔ restored.
- **User data:** settings, preferences, pantry, cooking log, learned flags — local only, included in backups.
