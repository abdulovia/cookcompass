package shops

import (
	"context"
)

// BuyLink is where an ingredient can be bought: either the exact product
// page, or a shop search when no catalogued product matches.
type BuyLink struct {
	URL   string
	Exact bool
	Shop  string // shop name for tooltips and labels ("" for generic products)
}

// Resolver builds buy links for ingredients. It prefers an exact product URL
// (the user's shop first, then the country's generic catalog, then any other
// shop) and falls back to the selected shop's search.
type Resolver struct {
	Shops       *Store
	ProductURLs ProductFinder
}

// ProductFinder answers "is there a product page for this ingredient?".
// catalog.Store implements it.
type ProductFinder interface {
	ProductURL(ctx context.Context, country string, shopID, ingredientID int64) (url string, shop string, err error)
}

// Link resolves the buy target for one ingredient. query is the localized
// ingredient name used for shop searches. ok is false when nothing useful is
// known (no product URL and no selected shop).
func (r *Resolver) Link(ctx context.Context, country string, shopID, ingredientID int64, query string) (BuyLink, bool) {
	if country == "" {
		return BuyLink{}, false
	}
	if ingredientID != 0 && r.ProductURLs != nil {
		if u, shop, err := r.ProductURLs.ProductURL(ctx, country, shopID, ingredientID); err == nil && u != "" {
			return BuyLink{URL: u, Exact: true, Shop: shop}, true
		}
	}
	if shopID != 0 && r.Shops != nil {
		sh, err := r.Shops.Get(ctx, shopID)
		if err == nil && sh.Active {
			if u := sh.SearchURLFor(query); u != "" {
				return BuyLink{URL: u, Exact: false, Shop: sh.Name}, true
			}
		}
	}
	return BuyLink{}, false
}
