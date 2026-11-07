-- +goose Up
CREATE TABLE cuisines (
    id   INTEGER PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL
);

-- Canonical ingredients shared by recipes and the product catalog.
CREATE TABLE ingredients (
    id       INTEGER PRIMARY KEY,
    slug     TEXT NOT NULL UNIQUE,
    name     TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'other',
    staple   INTEGER NOT NULL DEFAULT 0 -- assumed present in every kitchen
);

CREATE TABLE ingredient_aliases (
    alias         TEXT PRIMARY KEY, -- normalized, lowercase
    ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE
);

CREATE TABLE ingredient_i18n (
    ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
    locale        TEXT NOT NULL,
    name          TEXT NOT NULL,
    PRIMARY KEY (ingredient_id, locale)
);

CREATE TABLE ingredient_substitutes (
    ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
    substitute_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
    ratio         TEXT NOT NULL DEFAULT '1:1',
    PRIMARY KEY (ingredient_id, substitute_id)
);

CREATE TABLE recipes (
    id            INTEGER PRIMARY KEY,
    slug          TEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    cuisine_id    INTEGER REFERENCES cuisines(id),
    meal_times    TEXT NOT NULL DEFAULT '', -- comma list: breakfast,lunch,dinner,snack
    difficulty    TEXT NOT NULL CHECK (difficulty IN ('easy', 'medium', 'hard')),
    prep_minutes  INTEGER NOT NULL DEFAULT 0,
    cook_minutes  INTEGER NOT NULL DEFAULT 0,
    servings      INTEGER NOT NULL DEFAULT 1,
    video_url     TEXT NOT NULL DEFAULT '',
    image_url     TEXT NOT NULL DEFAULT '',
    steps_json    TEXT NOT NULL DEFAULT '[]',
    calories      REAL NOT NULL DEFAULT 0,
    protein       REAL NOT NULL DEFAULT 0,
    fat           REAL NOT NULL DEFAULT 0,
    carbs         REAL NOT NULL DEFAULT 0,
    fiber         REAL NOT NULL DEFAULT 0,
    sugar         REAL NOT NULL DEFAULT 0,
    sodium        REAL NOT NULL DEFAULT 0, -- mg
    saturated_fat REAL NOT NULL DEFAULT 0,
    source        TEXT NOT NULL DEFAULT 'user', -- seed | scraped:<domain> | user
    source_url    TEXT NOT NULL DEFAULT '',
    canonical_url TEXT,
    dedup_key     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'approved' CHECK (status IN ('pending', 'approved', 'rejected')),
    needs_review  INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    deleted_at    TEXT
);
CREATE UNIQUE INDEX recipes_canonical_url ON recipes (canonical_url) WHERE canonical_url IS NOT NULL;
CREATE INDEX recipes_dedup_key ON recipes (dedup_key);
CREATE INDEX recipes_visible ON recipes (status, deleted_at);

CREATE TABLE recipe_ingredients (
    id            INTEGER PRIMARY KEY,
    recipe_id     INTEGER NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    position      INTEGER NOT NULL,
    ingredient_id INTEGER REFERENCES ingredients(id),
    raw           TEXT NOT NULL DEFAULT '',
    quantity      REAL,
    unit          TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    essential     INTEGER NOT NULL DEFAULT 1,
    needs_review  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX recipe_ingredients_recipe ON recipe_ingredients (recipe_id, position);
CREATE INDEX recipe_ingredients_ingredient ON recipe_ingredients (ingredient_id);

CREATE TABLE recipe_i18n (
    recipe_id        INTEGER NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    locale           TEXT NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    steps_json       TEXT NOT NULL DEFAULT '[]',
    ingredients_json TEXT NOT NULL DEFAULT '[]', -- localized display lines, aligned by position
    PRIMARY KEY (recipe_id, locale)
);

-- Append-only history. Rows are only ever inserted, or pruned beyond the
-- configured retention; never modified.
CREATE TABLE recipe_revisions (
    id            INTEGER PRIMARY KEY,
    recipe_id     INTEGER NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    rev           INTEGER NOT NULL,
    source        TEXT NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    applied       INTEGER NOT NULL DEFAULT 1, -- 0 = scraped change withheld because a user edit wins
    snapshot_json TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (recipe_id, rev)
);
-- +goose StatementBegin
CREATE TRIGGER recipe_revisions_append_only BEFORE UPDATE ON recipe_revisions
BEGIN
    SELECT RAISE(ABORT, 'recipe_revisions is append-only');
END;
-- +goose StatementEnd

CREATE TABLE countries (
    code           TEXT PRIMARY KEY, -- ISO 3166-1 alpha-2
    name           TEXT NOT NULL,
    currency       TEXT NOT NULL,
    lat            REAL NOT NULL,
    lng            REAL NOT NULL,
    default_locale TEXT NOT NULL DEFAULT 'en'
);

CREATE TABLE cities (
    id           INTEGER PRIMARY KEY,
    country_code TEXT NOT NULL REFERENCES countries(code) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    lat          REAL NOT NULL,
    lng          REAL NOT NULL,
    UNIQUE (country_code, name)
);

CREATE TABLE products (
    id            INTEGER PRIMARY KEY,
    country_code  TEXT NOT NULL REFERENCES countries(code),
    sku           TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'other',
    ingredient_id INTEGER REFERENCES ingredients(id),
    price         REAL NOT NULL DEFAULT 0,
    currency      TEXT NOT NULL DEFAULT '',
    unit          TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT 'user', -- seed | scraped:<domain> | file:<name> | user
    source_url    TEXT NOT NULL DEFAULT '',
    dedup_key     TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'approved' CHECK (status IN ('pending', 'approved', 'rejected')),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    deleted_at    TEXT,
    UNIQUE (country_code, dedup_key)
);
CREATE INDEX products_availability ON products (country_code, ingredient_id, status, deleted_at);

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE user_settings (
    user_id          INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    country_code     TEXT REFERENCES countries(code),
    city_id          INTEGER REFERENCES cities(id) ON DELETE SET NULL,
    units            TEXT NOT NULL DEFAULT 'metric' CHECK (units IN ('metric', 'imperial')),
    locale           TEXT NOT NULL DEFAULT '',
    show_unavailable INTEGER NOT NULL DEFAULT 0,
    height_cm        REAL NOT NULL DEFAULT 0,
    weight_kg        REAL NOT NULL DEFAULT 0,
    age              INTEGER NOT NULL DEFAULT 0,
    sex              TEXT NOT NULL DEFAULT '',
    activity         TEXT NOT NULL DEFAULT '',
    goal             TEXT NOT NULL DEFAULT '',
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE user_dislikes (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL DEFAULT 'dislike' CHECK (kind IN ('dislike', 'allergy')),
    PRIMARY KEY (user_id, ingredient_id)
);

CREATE TABLE user_pantry (
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, ingredient_id)
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    expires_at TEXT NOT NULL
);

CREATE TABLE cooked_log (
    id        INTEGER PRIMARY KEY,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipe_id INTEGER NOT NULL REFERENCES recipes(id),
    cooked_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX cooked_log_user ON cooked_log (user_id, cooked_at);

CREATE TABLE learned (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipe_id  INTEGER NOT NULL REFERENCES recipes(id),
    learned_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (user_id, recipe_id)
);

-- +goose Down
DROP TABLE learned;
DROP TABLE cooked_log;
DROP TABLE sessions;
DROP TABLE user_pantry;
DROP TABLE user_dislikes;
DROP TABLE user_settings;
DROP TABLE users;
DROP TABLE products;
DROP TABLE cities;
DROP TABLE countries;
DROP TABLE recipe_revisions;
DROP TABLE recipe_i18n;
DROP TABLE recipe_ingredients;
DROP TABLE recipes;
DROP TABLE ingredient_substitutes;
DROP TABLE ingredient_i18n;
DROP TABLE ingredient_aliases;
DROP TABLE ingredients;
DROP TABLE cuisines;
