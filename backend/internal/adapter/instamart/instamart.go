// Package instamart implements the Swiggy Instamart adapter.
//
// Two quirks drive the shape of this code:
//   - Swiggy blocks legacy headless Chrome outright; the browser package runs
//     --headless=new, which it accepts.
//   - Navigating straight to the search route returns 403. Loading the
//     Instamart home page first and then moving to search is accepted, so the
//     warm-up navigation below is load-bearing, not politeness.
package instamart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
)

const (
	origin    = "https://www.swiggy.com"
	homeURL   = origin + "/instamart"
	searchAPI = "/api/instamart/search/v2"
	mediaBase = "https://media-assets.swiggy.com/swiggy/image/upload/"

	warmDelay = 7 * time.Second
	maxWait   = 20 * time.Second
)

type Adapter struct{ pool *browser.Pool }

func New(pool *browser.Pool) *Adapter { return &Adapter{pool: pool} }

func (a *Adapter) Platform() adapter.Platform { return adapter.Instamart }

type searchResponse struct {
	Data struct {
		Cards []struct {
			Card struct {
				Card struct {
					GridElements struct {
						InfoWithStyle struct {
							Items []item `json:"items"`
						} `json:"infoWithStyle"`
					} `json:"gridElements"`
				} `json:"card"`
			} `json:"card"`
		} `json:"cards"`
	} `json:"data"`
}

type item struct {
	DisplayName string      `json:"displayName"`
	Brand       string      `json:"brand"`
	InStock     bool        `json:"inStock"`
	ProductID   string      `json:"productId"`
	Variations  []variation `json:"variations"`
}

type variation struct {
	SkuID               string   `json:"skuId"`
	SpinID              string   `json:"spinId"`
	QuantityDescription string   `json:"quantityDescription"`
	DisplayName         string   `json:"displayName"`
	BrandName           string   `json:"brandName"`
	ImageIDs            []string `json:"imageIds"`
	PodID               string   `json:"podId"`
	Price               struct {
		MRP        money `json:"mrp"`
		OfferPrice money `json:"offerPrice"`
	} `json:"price"`
	Inventory struct {
		InStock bool `json:"inStock"`
	} `json:"inventory"`
}

// money is Swiggy's price envelope: whole rupees in Units, sub-rupee in Nanos.
type money struct {
	Units string `json:"units"`
	Nanos int    `json:"nanos"`
}

func (m money) paise() int {
	rupees, _ := strconv.Atoi(m.Units)
	return rupees*100 + m.Nanos/10_000_000
}

func (a *Adapter) Search(ctx context.Context, query string, loc adapter.Location) ([]adapter.Product, error) {
	sess, err := a.pool.NewSession(ctx, origin, loc.Lat, loc.Lon)
	if err != nil {
		return nil, fmt.Errorf("instamart session: %w", err)
	}
	defer sess.Close()

	sess.Capture(searchAPI)

	if err := sess.Navigate(homeURL, warmDelay); err != nil {
		return nil, fmt.Errorf("instamart warm-up: %w", err)
	}

	searchURL := origin + "/instamart/search?custom_back=true&query=" + url.QueryEscape(query)
	if err := sess.NavigateAndWait(searchURL, searchAPI, maxWait); err != nil {
		return nil, fmt.Errorf("instamart navigate: %w", err)
	}

	payloads := sess.Payloads(searchAPI)
	if len(payloads) == 0 {
		return nil, fmt.Errorf("instamart: no search payload captured")
	}

	seen := map[string]bool{}
	var out []adapter.Product
	for _, p := range payloads {
		var sr searchResponse
		if err := json.Unmarshal(p, &sr); err != nil {
			continue
		}
		for _, c := range sr.Data.Cards {
			for _, it := range c.Card.Card.GridElements.InfoWithStyle.Items {
				for _, v := range it.Variations {
					if v.SkuID == "" || seen[v.SkuID] {
						continue
					}
					seen[v.SkuID] = true

					var img string
					if len(v.ImageIDs) > 0 {
						img = mediaBase + v.ImageIDs[0]
					}
					deep := origin + "/instamart/item/" + v.SpinID
					if v.PodID != "" {
						deep += "?storeId=" + v.PodID
					}

					out = append(out, adapter.Product{
						Platform:   adapter.Instamart,
						ID:         v.SkuID,
						Name:       firstNonEmpty(v.DisplayName, it.DisplayName),
						Brand:      firstNonEmpty(v.BrandName, it.Brand),
						Variant:    v.QuantityDescription,
						ImageURL:   img,
						PricePaise: v.Price.OfferPrice.paise(),
						MRPPaise:   v.Price.MRP.paise(),
						InStock:    v.Inventory.InStock && it.InStock,
						DeepLink:   deep,
						StoreID:    v.PodID,
					})
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("instamart: payload captured but no products parsed")
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
