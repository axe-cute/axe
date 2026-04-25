import { series as seriesApi } from "@/lib/api";
import { Hero } from "@/components/Hero";
import { Row } from "@/components/Row";
import type { Series } from "@/lib/types";

// ISR: regenerate the static home page at most once per 60s. This turns
// what would be a dynamic render (DB hit per request) into a single DB hit
// per minute across all readers. Clicking a new link still revalidates
// in the background — users never see a stale page for long.
export const revalidate = 60;

async function safe<T>(p: Promise<T>, fallback: T): Promise<T> {
  try {
    return await p;
  } catch {
    return fallback;
  }
}

export default async function HomePage() {
  // Two parallel fetches: catalog (pulls 18 newest) + trending (top-10 by
  // score). Both opt into ISR (60s) so the home SSR cost is ~2 DB reads/min
  // regardless of concurrent user count.
  const [catalog, trending] = await Promise.all([
    safe(seriesApi.list({ limit: 18, revalidate: 60 }), { data: [] as Series[], total: 0, page: 1, limit: 18 }),
    safe(seriesApi.trending(10, 60), { data: [] as Series[], total: 0 }),
  ]);
  const all = catalog.data;
  const featured = trending.data[0] ?? all[0] ?? null;
  const ongoing = all.filter((s) => s.status === "ongoing").slice(0, 10);
  const completed = all.filter((s) => s.status === "completed").slice(0, 10);

  return (
    <>
      <Hero featured={featured} />
      {all.length === 0 ? (
        <div className="container-gutter py-24 text-center">
          <div className="label-eyebrow">No data</div>
          <h2 className="mt-2 text-xl font-semibold">Catalog is empty</h2>
          <p className="mt-2 text-fg-muted max-w-md mx-auto">
            Run <span className="font-mono text-fg">make seed</span> to populate
            the database with 8 demo series and episodes.
          </p>
        </div>
      ) : (
        <>
          {/*
            Trending uses the /trending endpoint, which reads a denormalised
            score column (maintained by internal/jobs/trending.go). This is
            an O(1) indexed lookup even at 100k+ series.
          */}
          {trending.data.length > 0 ? (
            <Row title="Trending now" series={trending.data} href="/browse" />
          ) : (
            <Row title="New arrivals" series={all.slice(0, 10)} href="/browse" />
          )}
          <Row title="Ongoing" series={ongoing} href="/browse?status=ongoing" />
          <Row
            title="Completed series"
            series={completed}
            href="/browse?status=completed"
          />
        </>
      )}
    </>
  );
}
