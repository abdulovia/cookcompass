-- +goose Up
-- Personal activity: favorites, shopping list, cooked-log ratings.
CREATE TABLE favorites (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipe_id  INTEGER NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (user_id, recipe_id)
);

-- One row per ingredient line the user wants to buy. Rows keep the source
-- recipe and the canonical ingredient when known, so the list can offer
-- buy links and "already at home" actions.
CREATE TABLE shopping_items (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipe_id     INTEGER REFERENCES recipes(id) ON DELETE SET NULL,
    ingredient_id INTEGER REFERENCES ingredients(id) ON DELETE SET NULL,
    text          TEXT NOT NULL,
    bought        INTEGER NOT NULL DEFAULT 0,
    position      INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX shopping_items_user ON shopping_items (user_id, bought, position);

ALTER TABLE cooked_log ADD COLUMN rating INTEGER; -- 1..5 stars, optional
ALTER TABLE cooked_log ADD COLUMN note TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE cooked_log DROP COLUMN note;
ALTER TABLE cooked_log DROP COLUMN rating;
DROP INDEX shopping_items_user;
DROP TABLE shopping_items;
DROP TABLE favorites;
