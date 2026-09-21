# kahan hai

Search a product once, see which quick-commerce apps actually have it, and
what each of them charges.

| | |
|---|---|
| Backend | Go, `chromedp`, SQLite (`modernc.org/sqlite`, pure Go) |
| Frontend | Next.js 15 + Tailwind 4 |
| Runtime | Docker Compose, running API, Chrome and web as three services |

## Quick start

```bash
make up
```

That builds and starts everything. Then open <http://localhost:3000>.

```bash
make help       # every available target
make logs       # follow logs
make down       # stop (your price history is kept)
```

Set your delivery point before the first run, because every result is resolved
against it:

```bash
cp .env.example .env   # then edit DEFAULT_LAT / DEFAULT_LON
```

## Platform support

| Platform | Status | Notes |
|---|---|---|
| Blinkit | Working | Intercepts `POST /v1/layout/search` |
| Swiggy Instamart | Working | Intercepts `POST /api/instamart/search/v2` |
| Zepto | Working | Intercepts the `bff-gateway` search API |
| Flipkart Minutes | Working | Drives the address picker, then intercepts `POST /api/4/page/fetch` |
| BigBasket | Not attempted | Akamai blocks datacenter IPs at the edge |
| Amazon Now | Not attempted | SSR-only, strongest bot detection of the set |

## How it works, and why

Collection runs through a **real Chrome**, not an HTTP client. That is not the
obvious choice, so it is worth saying why:

- Blinkit sits behind Cloudflare. A plain Go client gets `403`. So does a client
  that spoofs Chrome's TLS/JA3 fingerprint. Real Chrome passes.
- Swiggy blocks *legacy* headless Chrome explicitly ("your request looks
  automated"). Chrome's newer `--headless=new` is accepted.

So each adapter drives a browser session, but it does **not** scrape the DOM.
It registers a URL pattern, lets the page make its own API calls, and reads
those JSON responses. You get the platform's own clean, numeric payload
(`price`, `mrp`, `unit`, `inventory`, store id) instead of parsing styled markup
that breaks on every redesign.

```
Next.js  ──▶  Go API  ──▶  registry (parallel fan-out)  ──▶  adapter ──▶ Chrome
                 │                                                        │
                 └── SQLite (every observation logged) ◀──── JSON payload ┘
```

One adapter failing never fails a search: each platform's error is reported in
its own column while the others render normally.

### Location

Every platform resolves a lat/lon to a nearby dark store server-side, so results
are only valid for one point. There is no such thing as city-wide availability,
which is also why nothing can be pre-crawled and cached.

Three of the four accept the coordinates we override in the browser and get on
with it. Flipkart Minutes does not: until a store is bound to the session it
redirects every Minutes URL to an address picker. The adapter clicks that
picker's "Use my current location", which reads the same overridden
coordinates, so the location still comes from `DEFAULT_LAT`/`DEFAULT_LON`. It
just takes a click and a few extra seconds to get there. Minutes is
correspondingly the slowest adapter.

### Results ordering

Availability is the first question, not price. These platforms run fuzzy search
over one dark store's inventory: ask for "amul gold 1l" and a store that does
not stock it answers with whatever it does have, so the same query returns a
different set on every app. Which app has your thing at all is the answer you
need before any price is worth reading.

So each column is shown in **that platform's own relevance order**, untouched.
Re-sorting by price actively destroys the answer: the cheapest loosely related
item floats to the top while the thing you searched for sinks out of the
visible rows. Out-of-stock items keep their position too, marked in place, so
position always means relevance and nothing else.

For the same reason nothing here declares a single cheapest-anywhere winner.
There is no product matching across platforms, so a "winner" would be comparing
a 200ml sachet on one app against a 1L carton on another. Eight rows per app
sit side by side with photos and prices, and the comparison is yours to make.

### Price history

Every search writes each result to SQLite with a timestamp. Nothing reads it
yet beyond `GET /api/history`, but the data is accumulating from day one, which
is what makes price charts and drop alerts cheap to add later.

```bash
make db-stats   # how many observations recorded, per platform
make db-copy    # pull the database out of the volume (WAL included)
```

## Working on adapters

Adapters break when a platform changes its payload. Run one directly instead of
going through the server:

```bash
make probe P=blinkit Q="amul milk"
make probe-headful P=zepto      # watch the browser do it
make probe-dump P=minutes Q="maggi"  # save every intercepted payload to ./dump
```

Every adapter failure eventually reduces to "the JSON moved": a widget renamed,
a field nested one level deeper, a response that never arrived. By the time an
adapter reports `no products parsed` the browser is gone and so is the
evidence, which is what `probe-dump` exists to keep:

```bash
make probe-dump P=minutes Q="maggi noodles"
jq '.RESPONSE.slots[].widget.type' dump/www.flipkart.com-api-4-page-fetch-001.json
```

It is the `KH_DUMP_DIR` environment variable underneath, read in the one place
every capture passes through, so it works for the server too:

```bash
KH_DUMP_DIR=/tmp/kh make run-api
```

A saved payload also makes a good test fixture: parsing is a pure function, so
a captured response turns a 25 second live check into a millisecond one.

```bash
make test
```

Each adapter is one file behind one interface (`adapter.Adapter`), so a broken
platform is an isolated fix.

## API

```
GET /api/health
GET /api/search?q=coca+cola&lat=28.6315&lon=77.2167
GET /api/history?platform=blinkit&productId=1406720
```

## Scope

This is a personal tool for one household. It reads the same public endpoints
the platforms' own web apps call, at human search volume. It is not affiliated
with any of them, and running it at scale or as a public service would be a
different thing entirely, both technically (you would be fighting bot
detection continuously) and legally (their terms prohibit automated access).
