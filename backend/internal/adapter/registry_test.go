package adapter

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stub is an adapter that returns whatever it is given, so the registry can be
// tested without a browser.
type stub struct {
	platform Platform
	products []Product
	err      error
	delay    time.Duration
}

func (s stub) Platform() Platform { return s.platform }

func (s stub) Search(ctx context.Context, _ string, _ Location) ([]Product, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.products, s.err
}

func names(ps []Product) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

// The platform ranked these by relevance to the query. That order is the whole
// answer to "what did this app think you meant", so the registry must hand it
// back untouched: no price sort, no floating in-stock items to the top.
func TestSearchAllPreservesPlatformOrder(t *testing.T) {
	relevance := []Product{
		{Name: "Amul Gold Full Cream Milk", Variant: "1 ltr", PricePaise: 7200, InStock: true},
		{Name: "Amul Taaza Toned Milk", Variant: "1 ltr", PricePaise: 5900, InStock: false},
		{Name: "Amul Kool Badam", Variant: "180 ml", PricePaise: 2500, InStock: true},
	}

	// Captured before the call: an adapter hands the registry its own slice, so
	// anything sorting in place would mutate this and hide the regression.
	want := names(relevance)

	reg := NewRegistry(time.Second, stub{platform: Blinkit, products: relevance})
	results := reg.SearchAll(context.Background(), "amul milk 1l", Location{})

	got := names(results[0].Products)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order changed:\n got %v\nwant %v", got, want)
		}
	}
}

func TestSearchAllIsolatesFailures(t *testing.T) {
	reg := NewRegistry(time.Second,
		stub{platform: Blinkit, err: errors.New("cloudflare said no")},
		stub{platform: Zepto, products: []Product{{Name: "Amul Gold Milk"}}},
	)
	results := reg.SearchAll(context.Background(), "amul", Location{})

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Err == "" {
		t.Error("blinkit failed but no error was reported")
	}
	// A failed adapter must still serialize as an empty list rather than null,
	// because the frontend maps over it.
	if results[0].Products == nil {
		t.Error("failed adapter should carry an empty slice, not nil")
	}
	if results[1].Err != "" || len(results[1].Products) != 1 {
		t.Errorf("zepto should be unaffected, got %+v", results[1])
	}
}

// Results are indexed by adapter position, so a slow platform must not be able
// to reorder or displace a fast one.
func TestSearchAllKeepsAdapterPositions(t *testing.T) {
	reg := NewRegistry(time.Second,
		stub{platform: Blinkit, delay: 40 * time.Millisecond, products: []Product{{Name: "slow"}}},
		stub{platform: Zepto, products: []Product{{Name: "fast"}}},
	)
	results := reg.SearchAll(context.Background(), "x", Location{})

	if results[0].Platform != Blinkit || results[1].Platform != Zepto {
		t.Errorf("positions moved: %s, %s", results[0].Platform, results[1].Platform)
	}
	if results[0].Label != "Blinkit" || results[1].Label != "Zepto" {
		t.Errorf("labels wrong: %q, %q", results[0].Label, results[1].Label)
	}
}

func TestSearchAllTimesOutOneAdapter(t *testing.T) {
	reg := NewRegistry(30*time.Millisecond,
		stub{platform: Blinkit, delay: time.Second, products: []Product{{Name: "never arrives"}}},
		stub{platform: Zepto, products: []Product{{Name: "Amul"}}},
	)

	start := time.Now()
	results := reg.SearchAll(context.Background(), "x", Location{})

	if took := time.Since(start); took > 500*time.Millisecond {
		t.Errorf("the per-adapter timeout did not fire, took %s", took)
	}
	if results[0].Err == "" {
		t.Error("timed-out adapter should report an error")
	}
	if len(results[1].Products) != 1 {
		t.Error("the healthy adapter should still return its products")
	}
}

func TestDiscountPercent(t *testing.T) {
	cases := []struct {
		name       string
		price, mrp int
		want       int
	}{
		{"ordinary discount", 7200, 8300, 13},
		{"no mrp recorded", 7200, 0, 0},
		{"mrp equal to price", 7200, 7200, 0},
		{"mrp below price is nonsense and ignored", 7200, 5000, 0},
		{"free item", 0, 8300, 0},
		{"half price", 5000, 10000, 50},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := Product{PricePaise: c.price, MRPPaise: c.mrp}
			if got := p.DiscountPercent(); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestPlatformDisplayName(t *testing.T) {
	for p, want := range map[Platform]string{
		Blinkit:           "Blinkit",
		Instamart:         "Swiggy Instamart",
		Zepto:             "Zepto",
		Minutes:           "Flipkart Minutes",
		Platform("newco"): "newco",
	} {
		if got := p.DisplayName(); got != want {
			t.Errorf("%s: got %q, want %q", p, got, want)
		}
	}
}
