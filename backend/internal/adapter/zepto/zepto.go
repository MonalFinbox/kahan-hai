// Package zepto implements the Zepto adapter.
//
// The storefront is a React Server Components app, so there is no useful HTML
// to parse. It does however call a clean BFF endpoint
// (bff-gateway.zepto.com/user-search-service/api/v3/search) which we intercept.
// Note Zepto quotes money in paise already, unlike the other platforms.
package zepto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
)

const (
	origin    = "https://www.zepto.com"
	searchAPI = "/user-search-service/api/v3/search"
	cdnBase   = "https://cdn.zeptonow.com/production/"

	warmDelay = 6 * time.Second
	maxWait   = 20 * time.Second
)

type Adapter struct{ pool *browser.Pool }

func New(pool *browser.Pool) *Adapter { return &Adapter{pool: pool} }

func (a *Adapter) Platform() adapter.Platform { return adapter.Zepto }

type searchResponse struct {
	Layout []struct {
		WidgetName string `json:"widgetName"`
		Data       struct {
			Resolver struct {
				Data struct {
					Items []struct {
						ProductResponse productResponse `json:"productResponse"`
					} `json:"items"`
				} `json:"data"`
			} `json:"resolver"`
		} `json:"data"`
	} `json:"layout"`
}

type productResponse struct {
	ID      string `json:"id"`
	StoreID string `json:"storeId"`
	// Money here is already paise.
	DiscountedSellingPrice int  `json:"discountedSellingPrice"`
	MRP                    int  `json:"mrp"`
	OutOfStock             bool `json:"outOfStock"`
	AvailableQuantity      int  `json:"availableQuantity"`

	Product struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Brand string `json:"brand"`
	} `json:"product"`

	ProductVariant struct {
		ID                string `json:"id"`
		FormattedPacksize string `json:"formattedPacksize"`
		Images            []struct {
			Path string `json:"path"`
		} `json:"images"`
	} `json:"productVariant"`
}

func (a *Adapter) Search(ctx context.Context, query string, loc adapter.Location) ([]adapter.Product, error) {
	sess, err := a.pool.NewSession(ctx, origin, loc.Lat, loc.Lon)
	if err != nil {
		return nil, fmt.Errorf("zepto session: %w", err)
	}
	defer sess.Close()

	sess.Capture(searchAPI)

	if err := sess.Navigate(origin, warmDelay); err != nil {
		return nil, fmt.Errorf("zepto warm-up: %w", err)
	}

	searchURL := origin + "/search?query=" + url.QueryEscape(query)
	if err := sess.NavigateAndWait(searchURL, searchAPI, maxWait); err != nil {
		return nil, fmt.Errorf("zepto navigate: %w", err)
	}

	payloads := sess.Payloads(searchAPI)
	if len(payloads) == 0 {
		return nil, fmt.Errorf("zepto: no search payload captured")
	}

	seen := map[string]bool{}
	var out []adapter.Product
	for _, p := range payloads {
		var sr searchResponse
		if err := json.Unmarshal(p, &sr); err != nil {
			continue
		}
		for _, w := range sr.Layout {
			if !strings.HasPrefix(w.WidgetName, "SEARCHED_PRODUCTS") {
				continue
			}
			for _, it := range w.Data.Resolver.Data.Items {
				pr := it.ProductResponse
				if pr.ID == "" || pr.Product.Name == "" || seen[pr.ID] {
					continue
				}
				seen[pr.ID] = true

				var img string
				if len(pr.ProductVariant.Images) > 0 {
					img = cdnBase + pr.ProductVariant.Images[0].Path
				}

				out = append(out, adapter.Product{
					Platform:   adapter.Zepto,
					ID:         pr.ID,
					Name:       cleanName(pr.Product.Name),
					Brand:      pr.Product.Brand,
					Variant:    pr.ProductVariant.FormattedPacksize,
					ImageURL:   img,
					PricePaise: pr.DiscountedSellingPrice,
					MRPPaise:   pr.MRP,
					InStock:    !pr.OutOfStock && pr.AvailableQuantity > 0,
					DeepLink:   fmt.Sprintf("%s/pn/x/pvid/%s", origin, pr.ProductVariant.ID),
					StoreID:    pr.StoreID,
				})
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("zepto: payload captured but no products parsed")
	}
	return out, nil
}

// cleanName trims Zepto's pipe-padded SEO titles down to the product name.
func cleanName(s string) string {
	if i := strings.Index(s, "|"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
