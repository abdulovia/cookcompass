# Installing and running CookCompass

## Quick install (Docker)

Requirements: Docker with Compose v2, `make`, `git`. 1 GB RAM is plenty.

```sh
git clone https://github.com/abdulovia/cookcompass.git && cd cookcompass
make init          # .env with fresh passwords + the content/ folder; prints the admin login
make up-release    # prebuilt image from ghcr.io (no Go needed)
# or: make up      # build the image from source instead
```

Open http://localhost:8080 for the site and http://localhost:8080/admin for the admin area (user/password from
`make init`, stored in `.env`). The admin dashboard's **Getting started** card shows what is ready and what is left.
`make doctor` prints the same list in the terminal.

`make up` and `make up-release` run `make init` automatically if `.env` is missing. `make init` is safe to re-run:
it keeps existing values and only replaces empty or example secrets.

### What runs

| Service | Purpose | Data |
|---|---|---|
| `app` | the web app and CLI (`/app/cookcompass`) | volume `cookdata` (SQLite, caches, inboxes) |
| `minio` | S3 storage for recipe images (optional: without it images keep their original URLs) | volume `cookmedia` |
| `./content` | your recipes, data and text overrides, mounted read-only | the folder in your checkout |

Optional profiles: `docker compose --profile tls up -d` (local HTTPS via Caddy on https://localhost:8443) and
`--profile translate` (LibreTranslate on 127.0.0.1:15000; set `COOK_TRANSLATE_URL=http://libretranslate:5000` in `.env`;
the first start downloads ~1 GB of models).

## Configuration

Everything is optional and has a working default:

- `.env` is read by docker compose: port, admin user and password, MinIO credentials, image tag (`COOK_VERSION`), translation URL.
- `COOK_*` environment variables, or `config.yaml` (copy [`config.example.yaml`](../config.example.yaml)), configure the app itself:
  database path, default country and language, scraping politeness, storage, the TheMealDB key, multi-user mode.
- `content/` holds data and presentation: site name, colours, features, countries, shops, products, recipes, texts.
  See [content.md](content.md).

For a server reachable by others, also set `COOK_SECURE_COOKIES=true` behind HTTPS, consider
`COOK_MULTI_USER=true` (accounts with login), and keep the admin password from `make init`.

## Without Docker

Requires Go 1.24 (the build uses `GOTOOLCHAIN=local`).

```sh
make run           # builds bin/cookcompass and serves on :8080, data in ./data/
```

Without `COOK_ADMIN_PASSWORD`, `/admin` only answers requests from this machine. Image storage stays off unless
`COOK_S3_ENDPOINT` points at an S3-compatible server.

## Upgrading

```sh
git pull
make up            # or: make up-release (pulls the newest image)
```

Database migrations run on start. Your data (volumes) and `content/` are kept. Bundled recipes you have edited are
never overwritten. Pin a version with `COOK_VERSION=1.2.0` in `.env`.

## Backups

`make backup` writes a consistent SQLite snapshot to `backups/` (it works with or without the container running).
Back up `content/` and `.env` together with it. To restore, stop the app and copy a snapshot over the database
(`data/cookcompass.db`, or `/data/cookcompass.db` in the `cookdata` volume).

## Troubleshooting

| Symptom | Fix |
|---|---|
| `required variable … is missing a value: run "make init"` | run `make init` (creates `.env`) |
| page loads but no recipes | `make doctor`; usually `make seed`, or approve recipes in `/admin/queue` |
| `/admin` asks for a password you don't know | it's `COOK_ADMIN_PASSWORD` in `.env` |
| `/admin` returns 403 without Docker | no admin password set, so admin is loopback-only; open it from the same machine |
| images not mirrored | the admin checklist shows *Image storage*; check that `minio` is running (`docker compose ps`) |
| a content file didn't apply | restart the app; a broken recipe file is listed on the checklist; JSON lists merge by `slug`/`code` |
| port 8080 is taken | set `PORT=8090` in `.env` and run `make up` again |

Logs: `docker compose logs -f app` (or the terminal running `make run`).
