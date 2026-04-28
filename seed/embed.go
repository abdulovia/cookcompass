// Package seed embeds the bundled dataset: ingredients, cuisines, countries,
// product catalogs and recipes. It is loaded by `cookcompass seed` and on
// first start, and is the offline fallback for every scraper.
package seed

import "embed"

// FS holds the seed files.
//
//go:embed ingredients.json cuisines.json countries.json shops.csv products/*.csv recipes/*.json
var FS embed.FS
