"use client";

import { useState } from "react";
import {
  PLATFORM_STYLE,
  discountPercent,
  rupees,
  type PlatformResult,
  type Product,
  type SearchResponse,
} from "@/lib/types";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// How many rows each platform gets. These lists are relevance-ordered by the
// platform itself, and the match you wanted is near the top or not there at
// all, so a deep list adds noise rather than answers.
const PER_APP = 8;

export default function Home() {
  const [query, setQuery] = useState("");
  const [data, setData] = useState<SearchResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [coords, setCoords] = useState<{ lat: number; lon: number } | null>(null);

  async function search(e: React.FormEvent) {
    e.preventDefault();
    if (!query.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const params = new URLSearchParams({ q: query });
      if (coords) {
        params.set("lat", String(coords.lat));
        params.set("lon", String(coords.lon));
      }
      const res = await fetch(`${API}/api/search?${params}`);
      if (!res.ok) throw new Error(`search failed (${res.status})`);
      setData(await res.json());
    } catch (err) {
      setError(err instanceof Error ? err.message : "something went wrong");
    } finally {
      setLoading(false);
    }
  }

  function useMyLocation() {
    navigator.geolocation?.getCurrentPosition(
      (pos) => setCoords({ lat: pos.coords.latitude, lon: pos.coords.longitude }),
      () => setError("could not read your location"),
    );
  }

  const total = data?.results.reduce((n, r) => n + r.products.length, 0) ?? 0;

  return (
    <main className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <header className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight">kahan hai</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          One search, every quick-commerce app. See who has it, then compare.
        </p>
      </header>

      <form onSubmit={search} className="mb-6 flex flex-col gap-3 sm:flex-row">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="coca cola 1l, amul milk, maggi..."
          className="flex-1 rounded-lg border border-zinc-300 bg-white px-4 py-3 text-base outline-none transition focus:border-zinc-900 dark:border-zinc-700 dark:bg-zinc-900 dark:focus:border-zinc-100"
        />
        <button
          type="submit"
          disabled={loading}
          className="rounded-lg bg-zinc-900 px-6 py-3 font-medium text-white transition hover:bg-zinc-700 disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          {loading ? "Searching..." : "Search"}
        </button>
        <button
          type="button"
          onClick={useMyLocation}
          className="rounded-lg border border-zinc-300 px-4 py-3 text-sm transition hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          {coords ? "📍 Located" : "📍 Use my location"}
        </button>
      </form>

      {error && (
        <div className="mb-6 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">
          {error}
        </div>
      )}

      {loading && (
        <p className="text-sm text-zinc-500">
          Driving a real browser on each platform. This takes ~25s.
        </p>
      )}

      {data && !loading && (
        <>
          {/* Who has it. Each app's own best guess at the query, side by side.
              Deliberately not a single "cheapest" verdict: nothing here matches
              products across apps, so declaring a winner would be comparing a
              200ml sachet against a 1L carton. */}
          <div className="mb-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {data.results.map((r) => (
              <TopMatch key={r.platform} r={r} />
            ))}
          </div>

          <p className="mb-4 text-xs text-zinc-500">
            {total} results in {(data.tookMs / 1000).toFixed(1)}s ·{" "}
            {data.location.lat.toFixed(4)}, {data.location.lon.toFixed(4)} · each
            column in that app&apos;s own relevance order
          </p>

          <div className="grid gap-6 lg:grid-cols-2 xl:grid-cols-4">
            {data.results.map((r) => (
              <section
                key={r.platform}
                className="rounded-xl border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900"
              >
                <div className="mb-3 flex items-center justify-between">
                  <h2 className="flex items-center gap-2 font-semibold">
                    <span
                      className={`h-2.5 w-2.5 rounded-full ${PLATFORM_STYLE[r.platform]?.dot ?? "bg-zinc-400"}`}
                    />
                    {r.label}
                  </h2>
                  <span className="text-xs text-zinc-400">{r.tookMs}ms</span>
                </div>

                {r.error ? (
                  <p className="rounded-md bg-zinc-100 px-3 py-6 text-center text-xs text-zinc-500 dark:bg-zinc-800">
                    Unavailable right now
                  </p>
                ) : r.products.length === 0 ? (
                  <p className="px-3 py-6 text-center text-xs text-zinc-500">
                    Nothing matched
                  </p>
                ) : (
                  <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
                    {r.products.slice(0, PER_APP).map((p, i) => (
                      <ProductRow key={`${p.platform}-${p.id}`} p={p} rank={i + 1} />
                    ))}
                  </ul>
                )}
              </section>
            ))}
          </div>
        </>
      )}
    </main>
  );
}

// TopMatch is one app's answer to "do you have this at all".
function TopMatch({ r }: { r: PlatformResult }) {
  const style = PLATFORM_STYLE[r.platform];
  const top = r.products[0];

  return (
    <div
      className={`rounded-lg border border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900 ${
        top ? "" : "opacity-60"
      }`}
    >
      <p className="mb-2 flex items-center gap-2 text-xs font-medium text-zinc-500">
        <span className={`h-2 w-2 rounded-full ${style?.dot ?? "bg-zinc-400"}`} />
        {r.label}
      </p>

      {r.error ? (
        <p className="text-xs text-zinc-400">Unavailable</p>
      ) : !top ? (
        <p className="text-xs text-zinc-400">Nothing matched</p>
      ) : (
        <div className="flex items-center gap-2.5">
          <Thumb src={top.imageUrl} size="h-11 w-11" />
          <div className="min-w-0">
            <p className="truncate text-xs font-medium" title={top.name}>
              {top.name}
            </p>
            <p className="text-xs text-zinc-500">
              <span className="font-semibold text-zinc-900 dark:text-zinc-100">
                {rupees(top.pricePaise)}
              </span>
              {top.variant && <> · {top.variant}</>}
            </p>
          </div>
        </div>
      )}
    </div>
  );
}

function Thumb({ src, size }: { src?: string; size: string }) {
  // Hotlinked CDN assets go stale: a product dropped from a platform's
  // catalogue keeps its URL in our history long after the image 404s. Without
  // this an expired one renders as a blank white chip, which reads as "no
  // photo" in a layout where the photo is the point.
  const [failed, setFailed] = useState(false);

  if (!src || failed) {
    return <div className={`${size} shrink-0 rounded-md bg-zinc-100 dark:bg-zinc-800`} />;
  }

  // Plain <img> on a white chip: these are transparent PNGs cut for light
  // backgrounds, so they need one regardless of the page theme.
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
      className={`${size} shrink-0 rounded-md bg-white object-contain p-0.5`}
    />
  );
}

// ProductRow keeps its position regardless of price or stock: position means
// "how well this app thought it matched your search", and nothing else.
function ProductRow({ p, rank }: { p: Product; rank: number }) {
  const off = discountPercent(p);
  return (
    <li className="flex gap-3 py-3">
      <span className="w-3 shrink-0 pt-6 text-[10px] tabular-nums text-zinc-300 dark:text-zinc-600">
        {rank}
      </span>

      <Thumb src={p.imageUrl} size="h-16 w-16" />

      <div className="min-w-0 flex-1">
        <p className="line-clamp-2 text-sm font-medium" title={p.name}>
          {p.name}
        </p>
        <p className="text-xs text-zinc-500">{p.variant}</p>
        <div className="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <span className="text-base font-semibold">{rupees(p.pricePaise)}</span>
          {off > 0 && (
            <>
              <span className="text-xs text-zinc-400 line-through">
                {rupees(p.mrpPaise!)}
              </span>
              <span className="text-xs font-medium text-green-600 dark:text-green-400">
                {off}% off
              </span>
            </>
          )}
          {!p.inStock && (
            <span className="rounded bg-red-50 px-1.5 py-0.5 text-[10px] font-medium text-red-600 dark:bg-red-950 dark:text-red-400">
              out of stock
            </span>
          )}
        </div>
      </div>

      {p.deepLink && (
        <a
          href={p.deepLink}
          target="_blank"
          rel="noreferrer"
          className="self-center rounded-md border border-zinc-300 px-2.5 py-1 text-xs transition hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          Open
        </a>
      )}
    </li>
  );
}
