# Your content

Everything in this folder is layered over the data and texts bundled with CookCompass.
Add a file here, restart the app (`docker compose restart app`, or re-run `make run`), and it takes effect. No rebuild needed.
Nothing here is required: an empty folder means "use the bundled defaults".

| Put here | Effect |
|---|---|
| `site.yaml` | name, logo, tagline, brand colour, footer links, feature switches (shopping list, learning, goals, favorites, geolocation) — see [`seed/site.yaml`](../seed/site.yaml) |
| `recipes/<slug>.json` | extra recipes, same format as [`examples/recipes/buttered-toast.json`](examples/recipes/buttered-toast.json); loaded once (later edits happen in `/admin`) |
| `countries.json`, `cuisines.json`, `categories.json`, `ingredients.json` | entries **merged by `code`/`slug`**: list only what you add or change |
| `shops.csv`, `products/<cc>.csv` | extra shops and products; same country+slug / SKU updates the bundled row |
| `locales/<lang>.json` | override any UI text **key by key**, or add a whole new language |
| `static/...` | add or replace static files (`/static/...`) |

Only this README and `examples/` are versioned; everything else you put here stays local (see `.gitignore`).
Start from an example, one file at a time: `cp content/examples/site.yaml content/` (then edit it).

Full guide: [docs/content.md](../docs/content.md).
