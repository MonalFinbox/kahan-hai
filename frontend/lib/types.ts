export type Platform = "blinkit" | "instamart" | "zepto" | "minutes";

export interface Product {
  platform: Platform;
  id: string;
  name: string;
  brand?: string;
  variant?: string;
  imageUrl?: string;
  pricePaise: number;
  mrpPaise?: number;
  inStock: boolean;
  etaMinutes?: number;
  deepLink?: string;
  storeId?: string;
}

export interface PlatformResult {
  platform: Platform;
  label: string;
  products: Product[];
  error?: string;
  tookMs: number;
}

export interface SearchResponse {
  query: string;
  location: { lat: number; lon: number };
  tookMs: number;
  results: PlatformResult[];
}

export const rupees = (paise: number) =>
  (paise / 100).toLocaleString("en-IN", {
    style: "currency",
    currency: "INR",
    maximumFractionDigits: 2,
  });

export const discountPercent = (p: Product) => {
  if (!p.mrpPaise || p.mrpPaise <= p.pricePaise) return 0;
  return Math.round(((p.mrpPaise - p.pricePaise) / p.mrpPaise) * 100);
};

export const PLATFORM_STYLE: Record<Platform, { dot: string; ring: string }> = {
  blinkit: { dot: "bg-yellow-400", ring: "ring-yellow-400/30" },
  zepto: { dot: "bg-purple-500", ring: "ring-purple-500/30" },
  instamart: { dot: "bg-orange-500", ring: "ring-orange-500/30" },
  minutes: { dot: "bg-blue-500", ring: "ring-blue-500/30" },
};
