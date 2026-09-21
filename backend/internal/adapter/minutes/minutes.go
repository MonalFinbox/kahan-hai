// Package minutes implements the Flipkart Minutes adapter.
//
// Minutes is the awkward one of the four, for two reasons.
//
// Routing: Minutes stock is not on the ordinary search page. A plain
// /search?q=... returns the national marketplace, and so does adding
// marketplace=HYPERLOCAL on its own. The Minutes-scoped route is
// /search?q=...&marketplace=HYPERLOCAL&BU=Minutes, where the BU parameter is what
// actually scopes the query to the dark store.
//
// Location: unlike Blinkit, Instamart and Zepto, overriding the browser's
// coordinates is not enough. Until an address is bound to the session Flipkart
// redirects every Minutes URL to /hyperlocal-preview-page, which renders an
// address picker and nothing else. The redirect does preserve the destination
// in an originalUrl parameter, though, and the picker offers "Use my current
// location", which reads the geolocation we already override. Clicking it
// geocodes our coordinates, binds a store via /api/4/location/update, and
// returns to the original search URL. So the address picker is driven, but with
// one click rather than by typing an address.
//
// That click has to be a real one. The picker is react-native-web, whose
// responder system ignores dispatched MouseEvents; the click goes through CDP.
//
// Data comes from Flipkart's "rome" endpoint (POST /api/4/page/fetch), shaped
// {RESPONSE:{slots:[{widget:{type,data}}]}}. Products live in the
// PRODUCT_SUMMARY_EXTENDED widgets, which arrive across several responses as
// the results page pages itself in.
package minutes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
)

const (
	origin   = "https://www.flipkart.com"
	pageAPI  = "/api/4/page/fetch"
	widgetID = "PRODUCT_SUMMARY_EXTENDED"

	// useCurrentLocation is a leaf text node in the picker; the click lands on
	// the react-native-web pressable wrapping it.
	useCurrentLocation = "//div[text()='Use my current location']"

	pollInterval = 250 * time.Millisecond
	pickerWait   = 20 * time.Second
	clickTimeout = 15 * time.Second
	resultsWait  = 20 * time.Second
	// bindAttempts caps how many times we re-press the picker before giving up.
	bindAttempts = 3
	// maxScrolls bounds how deep into the results we go. This is a price
	// comparison, not a crawler, and the cheapest match is near the top.
	// batchWait is how long one scroll gets to produce more; drainCap bounds
	// the lot.
	maxScrolls = 4
	batchWait  = 8 * time.Second
	drainCap   = 25 * time.Second

	// Flipkart serves images from a templated URL; these fill its placeholders.
	imageWidth, imageHeight, imageQuality = "416", "416", "70"
)

type Adapter struct{ pool *browser.Pool }

func New(pool *browser.Pool) *Adapter { return &Adapter{pool: pool} }

func (a *Adapter) Platform() adapter.Platform { return adapter.Minutes }

func (a *Adapter) Search(ctx context.Context, query string, loc adapter.Location) ([]adapter.Product, error) {
	sess, err := a.pool.NewSession(ctx, origin, loc.Lat, loc.Lon)
	if err != nil {
		return nil, fmt.Errorf("minutes session: %w", err)
	}
	defer sess.Close()

	sess.Capture(pageAPI)

	searchURL := origin + "/search?q=" + url.QueryEscape(query) + "&marketplace=HYPERLOCAL&BU=Minutes"
	if err := sess.Navigate(searchURL, 0); err != nil {
		return nil, fmt.Errorf("minutes navigate: %w", err)
	}

	// The picker click can be swallowed: the button is live before its press
	// handler is, and under the four-way fan-out this tab is competing for the
	// same browser. A swallowed click fails silently: we simply stay on the
	// picker. So confirm results arrived, and press again if they did not.
	var lastErr error
	for attempt := 0; attempt < bindAttempts && ctx.Err() == nil; attempt++ {
		if err := a.bindStore(ctx, sess); err != nil {
			lastErr = err
			continue
		}
		if products := a.awaitResults(ctx, sess); len(products) > 0 {
			return products, nil
		}
		lastErr = fmt.Errorf("minutes: no products parsed (captured %d payloads)", len(sess.Payloads(pageAPI)))
	}
	return nil, lastErr
}

// bindStore resolves the address-picker interstitial.
//
// A fresh session carries no Flipkart cookies, so the picker is the normal
// landing point. A session that already has a store bound goes straight to
// results, and that is success too. Poll for whichever happens first instead of
// sleeping for the slowest case.
func (a *Adapter) bindStore(ctx context.Context, sess *browser.Session) error {
	deadline := time.Now().Add(pickerWait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(a.parse(sess.Payloads(pageAPI))) > 0 {
			return nil // already bound; results are arriving
		}
		if sess.HasXPath(useCurrentLocation) && geocoderReady(sess) {
			if err := sess.ClickXPath(useCurrentLocation, clickTimeout); err != nil {
				return fmt.Errorf("minutes: address picker shown but %w", err)
			}
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("minutes: neither results nor address picker appeared within %s", pickerWait)
}

// geocoderReady reports whether the picker can actually service a click yet.
//
// The button is in the DOM well before it works: pressing it turns our
// overridden coordinates into an address through Google's geocoder, which
// Flipkart loads asynchronously. Clicking before that script lands does
// nothing at all: no error, no navigation, just a page that never leaves the
// picker. So this is the precondition to wait on rather than a fixed sleep.
func geocoderReady(sess *browser.Session) bool {
	var ok bool
	if err := sess.Eval(`!!(window.google && google.maps && google.maps.Geocoder)`, &ok); err != nil {
		return false
	}
	return ok
}

// awaitResults waits for the first product payload, then briefly longer: the
// results page fetches itself in several pages, and stopping at the first would
// silently return a fraction of the catalogue.
func (a *Adapter) awaitResults(ctx context.Context, sess *browser.Session) []adapter.Product {
	deadline := time.Now().Add(resultsWait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil
		}
		if products := a.parse(sess.Payloads(pageAPI)); len(products) > 0 {
			return a.drain(ctx, sess, len(products))
		}
		time.Sleep(pollInterval)
	}
	return nil
}

// drain pulls in the later result batches.
//
// Minutes paginates its results: the first rome response carries one batch and
// the rest arrive as the page scrolls. Scrolling is done against the inner
// scroll view, because the document itself never scrolls on this app. After
// each scroll we wait for the product count to grow, and stop once a scroll
// stops producing anything.
func (a *Adapter) drain(ctx context.Context, sess *browser.Session, count int) []adapter.Product {
	products := a.parse(sess.Payloads(pageAPI))
	deadline := time.Now().Add(drainCap)

	for scrolls := 0; scrolls < maxScrolls && ctx.Err() == nil; scrolls++ {
		if err := sess.ScrollToBottom(); err != nil {
			break
		}

		grew := false
		for wait := time.Now().Add(batchWait); time.Now().Before(wait); {
			if time.Now().After(deadline) || ctx.Err() != nil {
				return products
			}
			time.Sleep(pollInterval)

			if next := a.parse(sess.Payloads(pageAPI)); len(next) > count {
				products, count, grew = next, len(next), true
				break
			}
		}
		if !grew {
			break
		}
	}
	return products
}

// romeResponse is the slice of Flipkart's page payload we care about. The real
// response is very large; everything not named here is ignored by encoding/json.
type romeResponse struct {
	Response struct {
		Slots []struct {
			Widget struct {
				Type string `json:"type"`
				Data struct {
					Products []struct {
						ProductInfo struct {
							Action struct {
								URL    string `json:"url"`
								Params struct {
									ShopID []string `json:"shopId"`
								} `json:"params"`
							} `json:"action"`
							Value productValue `json:"value"`
						} `json:"productInfo"`
					} `json:"products"`
				} `json:"data"`
			} `json:"widget"`
		} `json:"slots"`
	} `json:"RESPONSE"`
}

type productValue struct {
	ID           string `json:"id"`
	ProductBrand string `json:"productBrand"`
	SmartURL     string `json:"smartUrl"`

	Titles struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
	} `json:"titles"`

	Availability struct {
		DisplayState string `json:"displayState"`
	} `json:"availability"`

	Media struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"media"`

	Pricing struct {
		FinalPrice price   `json:"finalPrice"`
		MRP        *price  `json:"mrp"`
		Prices     []price `json:"prices"`
	} `json:"pricing"`
}

// price carries the amount as a decimal string ("36.00"), which we convert to
// integer paise directly rather than through a float.
type price struct {
	DecimalValue string `json:"decimalValue"`
	PriceType    string `json:"priceType"`
}

func (a *Adapter) parse(payloads [][]byte) []adapter.Product {
	seen := map[string]bool{}
	var out []adapter.Product

	for _, raw := range payloads {
		var rr romeResponse
		if err := json.Unmarshal(raw, &rr); err != nil {
			continue
		}
		for _, slot := range rr.Response.Slots {
			if slot.Widget.Type != widgetID {
				continue
			}
			for _, entry := range slot.Widget.Data.Products {
				v := entry.ProductInfo.Value
				if v.ID == "" || v.Titles.Title == "" || seen[v.ID] {
					continue
				}
				seen[v.ID] = true

				var storeID string
				if ids := entry.ProductInfo.Action.Params.ShopID; len(ids) > 0 {
					storeID = ids[0]
				}

				deepLink := v.SmartURL
				if deepLink == "" && entry.ProductInfo.Action.URL != "" {
					deepLink = origin + entry.ProductInfo.Action.URL
				}

				out = append(out, adapter.Product{
					Platform:   adapter.Minutes,
					ID:         v.ID,
					Name:       v.Titles.Title,
					Brand:      v.ProductBrand,
					Variant:    v.Titles.Subtitle,
					ImageURL:   imageURL(v),
					PricePaise: paise(v.Pricing.FinalPrice.DecimalValue),
					MRPPaise:   mrpPaise(v),
					InStock:    v.Availability.DisplayState == "IN_STOCK",
					DeepLink:   deepLink,
					StoreID:    storeID,
				})
			}
		}
	}
	return out
}

// mrpPaise prefers the dedicated mrp field, which is null on promoted listings
// even when they are discounted. Those carry the struck-off price in the prices
// array instead.
func mrpPaise(v productValue) int {
	if v.Pricing.MRP != nil {
		if p := paise(v.Pricing.MRP.DecimalValue); p > 0 {
			return p
		}
	}
	for _, p := range v.Pricing.Prices {
		if p.PriceType == "MRP" {
			return paise(p.DecimalValue)
		}
	}
	return 0
}

// paise converts a decimal rupee string ("36.00", "1,299.5") to integer paise
// without going through a float.
func paise(s string) int {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0
	}
	whole, frac, _ := strings.Cut(s, ".")
	rupees, err := strconv.Atoi(whole)
	if err != nil {
		return 0
	}
	// Normalise the fraction to exactly two digits: "5" is 50 paise, not 5.
	frac = (frac + "00")[:2]
	p, err := strconv.Atoi(frac)
	if err != nil {
		return rupees * 100
	}
	return rupees*100 + p
}

// imageURL fills the {@width}/{@height}/{@quality} placeholders Flipkart leaves
// in its CDN URLs for the client to size.
func imageURL(v productValue) string {
	if len(v.Media.Images) == 0 {
		return ""
	}
	r := strings.NewReplacer(
		"{@width}", imageWidth,
		"{@height}", imageHeight,
		"{@quality}", imageQuality,
	)
	return r.Replace(v.Media.Images[0].URL)
}
