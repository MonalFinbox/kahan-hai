"use client";

import { useState } from "react";
import {
  PLATFORM_STYLE,
  discountPercent,
  rupees,
  type Product,
  type SearchResponse,
} from "@/lib/types";

const API = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

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

  // The cheapest in-stock item anywhere: the answer to "where should I buy this".
  const best = data?.results
    .flatMap((r) => r.products)
    .filter((p) => p.inStock)
    .sort((a, b) => a.pricePaise - b.pricePaise)[0];

  return (
    <main className="mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8">
      <header className="mb-8">
        <h1 className="text-3xl font-bold tracking-tight">kahan hai</h1>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          One search, every quick-commerce app.
        </p>
      </header>

      <form onSubmit={search} className="mb-6 flex flex-col gap-3 sm:flex-row">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="coca cola 1l, amul milk, maggi…"
          className="flex-1 rounded-lg border border-zinc-300 bg-white px-4 py-3 text-base outline-none transition focus:border-zinc-900 dark:border-zinc-700 dark:bg-zinc-900 dark:focus:border-zinc-100"
        />
        <button
          type="submit"
          disabled={loading}
          className="rounded-lg bg-zinc-900 px-6 py-3 font-medium text-white transition hover:bg-zinc-700 disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          {loading ? "Searching…" : "Search"}
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
          Driving a real browser on each platform. This takes ~10s.
        </p>
      )}

      {data && !loading && (
        <>
          {best && (
            <div className="mb-6 rounded-lg border border-green-300 bg-green-50 px-4 py-3 dark:border-green-800 dark:bg-green-950">
              <span className="text-sm text-green-800 dark:text-green-300">
                Cheapest in stock: <strong>{best.name}</strong>{" "}
                {best.variant && <>({best.variant})</>} at{" "}
                <strong>{rupees(best.pricePaise)}</strong> on{" "}
                <strong className="capitalize">{best.platform}</strong>
              </span>
            </div>
          )}

          <p className="mb-4 text-xs text-zinc-500">
            {data.results.reduce((n, r) => n + r.products.length, 0)} results in{" "}
            {(data.tookMs / 1000).toFixed(1)}s · {data.location.lat.toFixed(4)},{" "}
            {data.location.lon.toFixed(4)}
          </p>

          <div className="grid gap-6 lg:grid-cols-2 xl:grid-cols-3">
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
                    {r.error.includes("not implemented")
                      ? "Not wired up yet"
                      : "Unavailable right now"}
                  </p>
                ) : r.products.length === 0 ? (
                  <p className="px-3 py-6 text-center text-xs text-zinc-500">No results</p>
                ) : (
                  <ul className="divide-y divide-zinc-100 dark:divide-zinc-800">
                    {r.products.slice(0, 10).map((p) => (
                      <ProductRow key={`${p.platform}-${p.id}`} p={p} isBest={p === best} />
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

function ProductRow({ p, isBest }: { p: Product; isBest: boolean }) {
  const off = discountPercent(p);
  return (
    <li className={`flex gap-3 py-3 ${!p.inStock ? "opacity-40" : ""}`}>
      {p.imageUrl ? (
        // Plain <img> on a white chip: these are hotlinked platform CDN assets,
        // and they are transparent PNGs cut for light backgrounds.
        <img
          src={p.imageUrl}
          alt=""
          loading="lazy"
          className="h-14 w-14 shrink-0 rounded-md bg-white object-contain p-0.5"
        />
      ) : (
        <div className="h-14 w-14 shrink-0 rounded-md bg-zinc-100 dark:bg-zinc-800" />
      )}

      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{p.name}</p>
        <p className="text-xs text-zinc-500">{p.variant}</p>
        <div className="mt-1 flex items-baseline gap-2">
          <span className={`text-sm font-semibold ${isBest ? "text-green-600 dark:text-green-400" : ""}`}>
            {rupees(p.pricePaise)}
          </span>
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
          {!p.inStock && <span className="text-xs text-red-500">out of stock</span>}
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
