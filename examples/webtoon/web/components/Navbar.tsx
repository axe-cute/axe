"use client";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { Search, BookMarked, User, LogOut } from "lucide-react";
import { clearSession, getUser } from "@/lib/api";
import clsx from "clsx";

type Me = { id: string; email: string; role: string } | null;

const links = [
  { href: "/", label: "Home" },
  { href: "/browse", label: "Browse" },
  { href: "/library", label: "Library" },
];

export default function Navbar() {
  const pathname = usePathname();
  const [me, setMe] = useState<Me>(null);
  const [scrolled, setScrolled] = useState(false);

  useEffect(() => {
    const sync = () => setMe(getUser());
    sync();
    window.addEventListener("webtoon:auth", sync);
    return () => window.removeEventListener("webtoon:auth", sync);
  }, []);

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      className={clsx(
        "sticky top-0 z-40 transition-colors duration-180 ease-out",
        scrolled
          ? "bg-bg/80 backdrop-blur border-b border-border"
          : "bg-transparent border-b border-transparent"
      )}
    >
      <div className="container-gutter h-14 flex items-center gap-8">
        <Link href="/" className="flex items-center gap-2 font-mono text-sm">
          <span className="inline-block h-2 w-2 rounded-sm bg-accent shadow-glow" />
          <span className="font-semibold tracking-tight">webtoon</span>
          <span className="text-fg-subtle">/axe</span>
        </Link>

        <nav className="hidden md:flex items-center gap-6 text-sm">
          {links.map((l) => {
            const active =
              l.href === "/" ? pathname === "/" : pathname.startsWith(l.href);
            return (
              <Link
                key={l.href}
                href={l.href}
                className={clsx(
                  "transition-colors duration-120 ease-out",
                  active ? "text-fg" : "text-fg-muted hover:text-fg"
                )}
              >
                {l.label}
                {active && (
                  <span className="block h-0.5 mt-2 -mb-[9px] bg-accent" />
                )}
              </Link>
            );
          })}
        </nav>

        <div className="ml-auto flex items-center gap-2">
          <Link
            href="/browse"
            className="btn-sm btn-ghost gap-2 hidden sm:inline-flex"
            aria-label="Search"
          >
            <Search size={14} />
            <span className="text-xs text-fg-subtle">⌘K</span>
          </Link>
          {me ? (
            <div className="flex items-center gap-2">
              {me.role === "admin" && (
                <Link
                  href="/admin"
                  className="btn-sm btn-ghost gap-2"
                  title="Admin"
                >
                  <span className="font-mono text-xs">admin</span>
                </Link>
              )}
              <Link href="/library" className="btn-sm btn-ghost gap-2">
                <BookMarked size={14} />
                <span className="hidden sm:inline">Library</span>
              </Link>
              <div className="flex items-center gap-2 pl-2 border-l border-border">
                <div className="h-7 w-7 rounded-full bg-accent-subtle text-accent flex items-center justify-center text-xs font-medium uppercase">
                  {me.email.slice(0, 1)}
                </div>
                <button
                  onClick={() => {
                    clearSession();
                    location.href = "/";
                  }}
                  className="btn-sm btn-ghost"
                  title="Sign out"
                >
                  <LogOut size={14} />
                </button>
              </div>
            </div>
          ) : (
            <>
              <Link href="/auth/login" className="btn-sm btn-ghost">
                Sign in
              </Link>
              <Link href="/auth/register" className="btn-sm btn-primary">
                <User size={14} />
                Sign up
              </Link>
            </>
          )}
        </div>
      </div>
    </header>
  );
}
