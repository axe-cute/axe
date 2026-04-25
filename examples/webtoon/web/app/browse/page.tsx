import Link from "next/link";
import { series as seriesApi } from "@/lib/api";
import { SeriesCard } from "@/components/SeriesCard";
import { GENRES, genreLabel } from "@/lib/genres";
import type { Series } from "@/lib/types";
import { ChevronLeft, ChevronRight, Filter } from "lucide-react";
import clsx from "clsx";

// Server-side browse with URL-state filters + offset pagination. Each page
// is an ISR render keyed on the full URL (page=3&genre=action&status=ongoing),
// so identical queries from different users share a single DB read per 60s.
export const revalidate = 60;

const PAGE_SIZE = 24;

type SearchParams = {
  page?: string;
  genre?: string;
  status?: string;
};

export default async function BrowsePage({
  searchParams,
}: {
  searchParams: Promise<SearchParams>;
}) {
  const sp = await searchParams;
  const page = Math.max(1, parseInt(sp.page ?? "1", 10) || 1);
  const genre = sp.genre ?? "";
  const status = sp.status ?? "";

  const res = await seriesApi
    .list({ page, limit: PAGE_SIZE, genre, status, revalidate: 60 })
    .catch(() => ({ data: [] as Series[], total: 0, page, limit: PAGE_SIZE }));

  const totalPages = Math.max(1, Math.ceil(res.total / PAGE_SIZE));
  const hasPrev = page > 1;
  const hasNext = page < totalPages;

  return (
    <div className="container-gutter py-10">
      <div className="mb-6">
        <div className="label-eyebrow">Catalog</div>
        <h1 className="mt-2 text-xl font-semibold">
          Browse {res.total.toLocaleString()} series
        </h1>
      </div>

      <div className="flex items-center gap-2 mb-6 flex-wrap">
        <Filter size={14} className="text-fg-subtle" />
        <FilterLink label="All genres" active={!genre} params={{ genre: "", status, page: "1" }} />
        {GENRES.map((g) => (
          <FilterLink
            key={g}
            label={genreLabel(g)}
            active={genre === g}
            params={{ genre: g, status, page: "1" }}
          />
        ))}
      </div>

      <div className="flex items-center gap-2 mb-8 flex-wrap">
        <FilterLink
          label="All status"
          active={!status}
          params={{ genre, status: "", page: "1" }}
        />
        {(["ongoing", "completed", "hiatus"] as const).map((s) => (
          <FilterLink
            key={s}
            label={s}
            active={status === s}
            params={{ genre, status: s, page: "1" }}
          />
        ))}
      </div>

      {res.data.length === 0 ? (
        <EmptyState />
      ) : (
        <>
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
            {res.data.map((s, i) => (
              <SeriesCard key={s.id} s={s} priority={i < 5} />
            ))}
          </div>

          <nav className="mt-10 flex items-center justify-between gap-3">
            <PageLink
              href={hasPrev ? buildHref({ page: page - 1, genre, status }) : null}
            >
              <ChevronLeft size={14} /> Prev
            </PageLink>
            <span className="text-sm text-fg-subtle font-mono">
              {page} / {totalPages}
            </span>
            <PageLink
              href={hasNext ? buildHref({ page: page + 1, genre, status }) : null}
            >
              Next <ChevronRight size={14} />
            </PageLink>
          </nav>
        </>
      )}
    </div>
  );
}

function buildHref({
  page,
  genre,
  status,
}: {
  page: number;
  genre: string;
  status: string;
}): string {
  const qs = new URLSearchParams();
  if (page > 1) qs.set("page", String(page));
  if (genre) qs.set("genre", genre);
  if (status) qs.set("status", status);
  const s = qs.toString();
  return s ? `/browse?${s}` : "/browse";
}

function FilterLink({
  label,
  active,
  params,
}: {
  label: string;
  active: boolean;
  params: { genre: string; status: string; page: string };
}) {
  const href = buildHref({
    page: parseInt(params.page, 10) || 1,
    genre: params.genre,
    status: params.status,
  });
  return (
    <Link
      href={href}
      scroll={false}
      className={clsx(
        "h-7 px-3 inline-flex items-center rounded text-xs font-medium uppercase tracking-[0.04em] border transition-colors duration-120",
        active
          ? "bg-accent-subtle border-accent/50 text-accent"
          : "bg-bg-elevated border-border text-fg-muted hover:text-fg hover:border-border-strong",
      )}
    >
      {label}
    </Link>
  );
}

function PageLink({
  href,
  children,
}: {
  href: string | null;
  children: React.ReactNode;
}) {
  if (!href) {
    return (
      <span className="btn-md btn-secondary opacity-40 cursor-not-allowed gap-2">
        {children}
      </span>
    );
  }
  return (
    <Link href={href} className="btn-md btn-secondary gap-2">
      {children}
    </Link>
  );
}

function EmptyState() {
  return (
    <div className="text-center py-24">
      <div className="label-eyebrow">No results</div>
      <h2 className="mt-2 text-lg font-semibold">Nothing matches your filters</h2>
      <p className="mt-2 text-fg-muted">
        Try clearing a filter —{" "}
        <Link href="/browse" className="text-accent hover:underline">
          reset
        </Link>
        .
      </p>
    </div>
  );
}
