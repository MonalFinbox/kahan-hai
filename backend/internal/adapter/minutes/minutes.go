// Package minutes is the Flipkart Minutes adapter. NOT YET IMPLEMENTED.
//
// What reconnaissance established, so whoever finishes this does not repeat it:
//
//   - The storefront entry point is
//     https://www.flipkart.com/flipkart-minutes-store?marketplace=HYPERLOCAL
//     A cold load of https://www.flipkart.com/minutes fails; it only works as a
//     client-side route reached from the marketplace shell.
//
//   - All page data flows through Flipkart's "rome" endpoint:
//     POST https://2.rome.api.flipkart.com/api/4/page/fetch
//     The response is {RESPONSE:{slots:[{widget:{type,data}}]}}. Intercepting it
//     works fine; the browser package already captures it.
//
//   - The blocker is location. Unlike the other three platforms, granting the
//     geolocation permission and overriding coordinates is NOT enough: Minutes
//     returns only USER_CURRENT_LOCATION_V2 / USER_ADDRESS_SELECTION_V3 widgets
//     until an address is explicitly confirmed through its picker UI. Driving
//     that picker (type into "Search by area, street name, pin code", choose a
//     suggestion, click Confirm) does work manually and yields a serviceable
//     store with a delivery-time badge, so the remaining work is scripting that
//     flow in chromedp and then finding the Minutes-scoped search route.
//     Plain /search?q=...&marketplace=HYPERLOCAL does not search Minutes stock.
//
//   - Once a store is bound, the product slots should appear in the same rome
//     payload and can be parsed the way the other adapters parse theirs.
package minutes

import (
	"context"
	"errors"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
)

// ErrNotImplemented is returned for every search until the address flow above
// is scripted. The API surfaces it per-platform, so the other three keep working.
var ErrNotImplemented = errors.New("flipkart minutes: adapter not implemented yet (needs address-picker flow, see package doc)")

type Adapter struct{ pool *browser.Pool }

func New(pool *browser.Pool) *Adapter { return &Adapter{pool: pool} }

func (a *Adapter) Platform() adapter.Platform { return adapter.Minutes }

func (a *Adapter) Search(context.Context, string, adapter.Location) ([]adapter.Product, error) {
	return nil, ErrNotImplemented
}
