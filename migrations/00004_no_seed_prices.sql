-- +goose Up
-- The bundled catalog is an availability baseline; its prices were
-- illustrative, not real. Clear them so only scanned, imported or
-- hand-entered prices are ever shown.
UPDATE products SET price = 0 WHERE source = 'seed';

-- +goose Down
-- Illustrative prices are not restored.
SELECT 1;
