package adapter

import (
	"context"
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
			// Deliberately left in the platform's own order.
			//
			// Each platform ran its own relevance ranking against the query, and
			// that ranking is the best answer available to "what did this app
			// think you meant". Re-sorting by price destroys it: the cheapest
			// loosely related item floats to the top and the thing actually
			// searched for sinks out of the visible rows. Availability comes
			// first, price is read off the results.
			results[i] = res
		}(i, a)
	}

	wg.Wait()
	return results
}
