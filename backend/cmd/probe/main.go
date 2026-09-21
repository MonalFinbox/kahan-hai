// Command probe runs one adapter directly, without the server. It is the
// fastest way to see whether a platform still parses after it changes.
//
//	go run ./cmd/probe -platform blinkit -q "coca cola 1l"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/blinkit"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/instamart"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/minutes"
	"github.com/monalbarse/kahan-hai/backend/internal/adapter/zepto"
	"github.com/monalbarse/kahan-hai/backend/internal/browser"
)

func main() {
	var (
		platform = flag.String("platform", "all", "blinkit|instamart|zepto|minutes|all")
		query    = flag.String("q", "coca cola 1l", "search query")
		lat      = flag.Float64("lat", 28.6315, "latitude")
		lon      = flag.Float64("lon", 77.2167, "longitude")
		headful  = flag.Bool("headful", false, "show the browser window (debugging)")
	)
	flag.Parse()

	pool := browser.NewPool(os.Getenv("CHROME_URL"), !*headful)
	defer pool.Close()

	all := map[string]adapter.Adapter{
		"blinkit":   blinkit.New(pool),
		"instamart": instamart.New(pool),
		"zepto":     zepto.New(pool),
		"minutes":   minutes.New(pool),
	}

	var chosen []adapter.Adapter
	if *platform == "all" {
		for _, k := range []string{"blinkit", "instamart", "zepto", "minutes"} {
			chosen = append(chosen, all[k])
		}
	} else {
		a, ok := all[*platform]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown platform %q\n", *platform)
			os.Exit(2)
		}
		chosen = append(chosen, a)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	reg := adapter.NewRegistry(90*time.Second, chosen...)
	for _, r := range reg.SearchAll(ctx, *query, adapter.Location{Lat: *lat, Lon: *lon}) {
		fmt.Printf("\n=== %-18s %4dms  n=%d\n", r.Label, r.TookMS, len(r.Products))
		if r.Err != "" {
			fmt.Printf("    ERROR: %s\n", r.Err)
		}
		for i, p := range r.Products {
			if i >= 8 {
				fmt.Printf("    … %d more\n", len(r.Products)-8)
				break
			}
			stock := "  "
			if !p.InStock {
				stock = "✗ "
			}
			fmt.Printf("  %s%-44s %-18s ₹%-8.2f %2d%% off\n",
				stock, trunc(p.Name, 44), trunc(p.Variant, 18),
				float64(p.PricePaise)/100, p.DiscountPercent())
		}
	}
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
