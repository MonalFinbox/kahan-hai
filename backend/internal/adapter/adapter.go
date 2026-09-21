// Package adapter defines the contract every quick-commerce platform integration
// implements, plus the normalized types the rest of the app works with.
package adapter

import (
	"context"
	"time"
)

type Platform string

const (
	Blinkit   Platform = "blinkit"
	Instamart Platform = "instamart"
	Zepto     Platform = "zepto"
	Minutes   Platform = "minutes"
)

// DisplayName is the human label shown in the UI.
func (p Platform) DisplayName() string {
	switch p {
	case Blinkit:
		return "Blinkit"
	case Instamart:
		return "Swiggy Instamart"
	case Zepto:
		return "Zepto"
	case Minutes:
		return "Flipkart Minutes"
	}
	return string(p)
}

// Location is the delivery point a search is run against. Every platform resolves
// this to a dark store server-side, so results are only valid for this exact point.
type Location struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Product is one normalized search hit.
//
// Money is in paise (integer) throughout to avoid float rounding on prices.
type Product struct {
	Platform Platform `json:"platform"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Brand    string   `json:"brand,omitempty"`
	// Variant is the pack size as the platform states it, e.g. "1 L", "330 ml".
	Variant  string `json:"variant,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`

	PricePaise int `json:"pricePaise"`
	MRPPaise   int `json:"mrpPaise,omitempty"`

	InStock    bool `json:"inStock"`
	ETAMinutes int  `json:"etaMinutes,omitempty"`

	// DeepLink opens this product in the platform's own web/app experience.
	DeepLink string `json:"deepLink,omitempty"`
	// StoreID is the resolved dark store, when the platform exposes it.
	StoreID string `json:"storeId,omitempty"`
}

// DiscountPercent returns the discount off MRP, or 0 when there is none.
func (p Product) DiscountPercent() int {
	if p.MRPPaise <= 0 || p.PricePaise <= 0 || p.MRPPaise <= p.PricePaise {
		return 0
	}
	return int(float64(p.MRPPaise-p.PricePaise) / float64(p.MRPPaise) * 100)
}

// Adapter is one platform integration. Implementations must be safe for
// concurrent use: the API fans out to all of them at once.
type Adapter interface {
	Platform() Platform
	Search(ctx context.Context, query string, loc Location) ([]Product, error)
}

// Result is one adapter's outcome in a fan-out. An adapter failing is normal and
// expected (blocked, changed payload, no serviceable store), so the error is
// carried per-platform rather than failing the whole search.
type Result struct {
	Platform Platform      `json:"platform"`
	Label    string        `json:"label"`
	Products []Product     `json:"products"`
	Err      string        `json:"error,omitempty"`
	Took     time.Duration `json:"-"`
	TookMS   int64         `json:"tookMs"`
}
