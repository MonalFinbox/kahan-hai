# kahan hai

Search a product once, see what it costs on every quick-commerce app that
delivers to your door.

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
| Flipkart Minutes | **Not implemented** | Needs its address-picker flow scripted, see `internal/adapter/minutes` |
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
