// Package blinkit implements the Blinkit adapter.
//
// Blinkit sits behind Cloudflare, so collection runs through a real browser.
// The search page fires POST /v1/layout/search and we read that response
// directly: it carries a clean numeric cart_item per product, which is far more
// stable to parse than the styled display fields next to it.
package blinkit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
)

const (
	origin      = "https://blinkit.com"
	searchAPI   = "/v1/layout/search"
	settleDelay = 8 * time.Second
)

type Adapter struct{ pool *browser.Pool }

func New(pool *browser.Pool) *Adapter { return &Adapter{pool: pool} }

func (a *Adapter) Platform() adapter.Platform { return adapter.Blinkit }

// searchResponse mirrors only the fields we consume.
type searchResponse struct {
	Response struct {
		Snippets []struct {
			WidgetType string `json:"widget_type"`
			Data       struct {
				ProductID  string `json:"product_id"`
				MerchantID string `json:"merchant_id"`
				IsSoldOut  bool   `json:"is_sold_out"`
				State      string `json:"product_state"`
				ETATag     struct {
					Title struct {
						Text string `json:"text"`
					} `json:"title"`
				} `json:"eta_tag"`
				ATC struct {
					AddToCart struct {
						CartItem cartItem `json:"cart_item"`
					} `json:"add_to_cart"`
				} `json:"atc_action"`
			} `json:"data"`
		} `json:"snippets"`
	} `json:"response"`
}

// cartItem is Blinkit's own normalized product payload: plain numbers and
// strings, no display formatting to strip.
type cartItem struct {
	ProductID   int    `json:"product_id"`
	MerchantID  int    `json:"merchant_id"`
	ProductName string `json:"product_name"`
	DisplayName string `json:"display_name"`
	Brand       string `json:"brand"`
	Unit        string `json:"unit"`
	Price       int    `json:"price"`
	MRP         int    `json:"mrp"`
	Inventory   int    `json:"inventory"`
	ImageURL    string `json:"image_url"`
}

func (a *Adapter) Search(ctx context.Context, query string, loc adapter.Location) ([]adapter.Product, error) {
	sess, err := a.pool.NewSession(ctx, origin, loc.Lat, loc.Lon)
	if err != nil {
		return nil, fmt.Errorf("blinkit session: %w", err)
	}
	defer sess.Close()

	sess.Capture(searchAPI)

	u := origin + "/s/?q=" + url.QueryEscape(query)
	if err := sess.NavigateAndWait(u, searchAPI, settleDelay); err != nil {
		return nil, fmt.Errorf("blinkit navigate: %w", err)
	}

	payloads := sess.Payloads(searchAPI)
	if len(payloads) == 0 {
		return nil, fmt.Errorf("blinkit: no search payload captured (page may have been challenged)")
	}

	seen := map[int]bool{}
	var out []adapter.Product
	for _, p := range payloads {
		var sr searchResponse
		if err := json.Unmarshal(p, &sr); err != nil {
			continue
		}
		for _, sn := range sr.Response.Snippets {
			ci := sn.Data.ATC.AddToCart.CartItem
			if ci.ProductID == 0 || ci.ProductName == "" || seen[ci.ProductID] {
				continue
			}
			seen[ci.ProductID] = true

			out = append(out, adapter.Product{
				Platform:   adapter.Blinkit,
				ID:         fmt.Sprint(ci.ProductID),
				Name:       firstNonEmpty(ci.DisplayName, ci.ProductName),
				Brand:      ci.Brand,
				Variant:    ci.Unit,
				ImageURL:   ci.ImageURL,
				PricePaise: ci.Price * 100,
				MRPPaise:   ci.MRP * 100,
				InStock:    !sn.Data.IsSoldOut && ci.Inventory > 0,
				DeepLink:   fmt.Sprintf("%s/prn/x/prid/%d", origin, ci.ProductID),
				StoreID:    fmt.Sprint(ci.MerchantID),
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("blinkit: payload captured but no products parsed")
	}
	return out, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
