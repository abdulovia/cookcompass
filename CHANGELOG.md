# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

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
