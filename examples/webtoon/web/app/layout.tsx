import type { Metadata } from "next";
import { Inter, JetBrains_Mono } from "next/font/google";
import "./globals.css";
import Navbar from "@/components/Navbar";
import { AuthProvider } from "@/components/AuthProvider";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-inter",
  display: "swap",
});
const mono = JetBrains_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: "webtoon — axe example",
  description:
    "Serialized stories, read at your pace. A reader-first webtoon platform built on the axe Go framework.",
  metadataBase: new URL("http://localhost:3000"),
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`${inter.variable} ${mono.variable}`}>
      {/*
        suppressHydrationWarning on <body> swallows the noisy mismatch caused
        by browser extensions (Grammarly, LanguageTool, Dashlane, etc.) that
        inject attributes like data-new-gr-c-s-check-loaded before React
        hydrates. This does NOT hide mismatches in our own component tree —
        React still warns for children. Scoped to body only, per React docs:
        https://react.dev/reference/react-dom/client/hydrateRoot#suppressing-unavoidable-hydration-mismatch-errors
      */}
      <body className="min-h-screen bg-bg text-fg" suppressHydrationWarning>
        <AuthProvider>
          <a
            href="#main"
            className="sr-only focus:not-sr-only fixed top-2 left-2 z-50 btn-sm btn-primary"
          >
            Skip to content
          </a>
          <Navbar />
          <main id="main">{children}</main>
          <footer className="container-gutter mt-24 py-12 text-sm text-fg-subtle border-t border-border flex flex-col md:flex-row gap-4 items-start md:items-center justify-between">
          <div>
            Built with <span className="font-mono text-fg">axe</span> · Next.js · Ent · PostgreSQL
          </div>
          <div className="flex gap-4">
            <a
              href="https://github.com/axe-cute/axe"
              className="hover:text-fg"
              rel="noreferrer"
            >
              GitHub
            </a>
            <a href="/health" className="hover:text-fg">
              API status
            </a>
          </div>
        </footer>
        </AuthProvider>
      </body>
    </html>
  );
}
