import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "kahan hai",
  description: "Compare quick-commerce prices across Blinkit, Zepto and Instamart",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-zinc-50 text-zinc-900 antialiased dark:bg-zinc-950 dark:text-zinc-100">
        {children}
      </body>
    </html>
  );
}
