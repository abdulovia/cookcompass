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
