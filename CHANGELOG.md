# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
