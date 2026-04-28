-- +goose Up
-- Grocery shops: every shop carries a search URL template so any ingredient
-- can get a "buy" link, even before its catalog has been scanned.
CREATE TABLE shops (
    id                 INTEGER PRIMARY KEY,
    country_code       TEXT NOT NULL REFERENCES countries(code) ON DELETE CASCADE,
    slug               TEXT NOT NULL,
    name               TEXT NOT NULL,
    website            TEXT NOT NULL DEFAULT '',
    search_url_template TEXT NOT NULL DEFAULT '', -- must contain {q}, e.g. https://shop.example/search?q={q}
    active             INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (country_code, slug)
);
CREATE INDEX shops_country ON shops (country_code, active, name);

ALTER TABLE products ADD COLUMN shop_id INTEGER REFERENCES shops(id) ON DELETE SET NULL;
ALTER TABLE products ADD COLUMN url TEXT NOT NULL DEFAULT ''; -- direct product page
CREATE INDEX products_shop ON products (shop_id, ingredient_id, status, deleted_at);

ALTER TABLE user_settings ADD COLUMN shop_id INTEGER REFERENCES shops(id) ON DELETE SET NULL;
ALTER TABLE user_settings ADD COLUMN strict_shop INTEGER NOT NULL DEFAULT 0; -- availability from my shop + generic rows only

-- +goose Down
ALTER TABLE user_settings DROP COLUMN strict_shop;
ALTER TABLE user_settings DROP COLUMN shop_id;
DROP INDEX products_shop;
ALTER TABLE products DROP COLUMN url;
ALTER TABLE products DROP COLUMN shop_id;
DROP INDEX shops_country;
DROP TABLE shops;
