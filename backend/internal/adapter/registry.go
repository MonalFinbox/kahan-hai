package adapter

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Registry fans a single query out to every adapter at once.
type Registry struct {
	adapters []Adapter
	timeout  time.Duration
}

func NewRegistry(timeout time.Duration, adapters ...Adapter) *Registry {
	return &Registry{adapters: adapters, timeout: timeout}
}

func (r *Registry) Platforms() []Platform {
	out := make([]Platform, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a.Platform())
	}
	return out
}

// SearchAll queries every platform concurrently. One adapter failing never
// fails the search: its error is reported in its own Result so the UI can show
// "unavailable" for that column while the rest render normally.
func (r *Registry) SearchAll(ctx context.Context, query string, loc Location) []Result {
	results := make([]Result, len(r.adapters))
	var wg sync.WaitGroup

	for i, a := range r.adapters {
		wg.Add(1)
		go func(i int, a Adapter) {
			defer wg.Done()

			actx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()

			start := time.Now()
			products, err := a.Search(actx, query, loc)
			took := time.Since(start)

			res := Result{
				Platform: a.Platform(),
				Label:    a.Platform().DisplayName(),
				Products: products,
				Took:     took,
				TookMS:   took.Milliseconds(),
			}
			if err != nil {
				res.Err = err.Error()
			}
			if res.Products == nil {
				res.Products = []Product{}
			}
			// Cheapest in-stock option first: that is the question being asked.
			sort.SliceStable(res.Products, func(x, y int) bool {
				px, py := res.Products[x], res.Products[y]
				if px.InStock != py.InStock {
					return px.InStock
				}
				return px.PricePaise < py.PricePaise
			})
			results[i] = res
		}(i, a)
	}

	wg.Wait()
	return results
}
