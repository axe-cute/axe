import Image from "next/image";
import Link from "next/link";
import { notFound } from "next/navigation";
import { series as seriesApi, episodes as episodesApi } from "@/lib/api";
import { BookmarkButton } from "@/components/BookmarkButton";
import { genreLabel } from "@/lib/genres";
import { Play, Clock } from "lucide-react";

// ISR: series detail is effectively static for long stretches (title,
// synopsis, episode list rarely change). 60s balances freshness with the
// ~100k concurrent reads this page handles on a real platform.
export const revalidate = 60;

export default async function SeriesPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;

  const [s, ep] = await Promise.all([
    seriesApi.get(id, 60).catch(() => null),
    // Limit 100 is the server cap — enough for most serialised webtoons.
    // For long-running series (>100 eps), add a "load more" UI.
    episodesApi.bySeries(id, { limit: 100, revalidate: 60 }).catch(() => ({ data: [], total: 0 })),
  ]);
  if (!s) return notFound();

  const eps = [...ep.data].sort((a, b) => a.episode_number - b.episode_number);
  const firstEpisode = eps[0];

  return (
    <div className="container-gutter py-10">
      <div className="grid md:grid-cols-[280px_1fr] gap-10">
        <div>
          <div className="relative aspect-[3/4] overflow-hidden rounded-md border border-border bg-bg-elevated shadow-md">
            <Image
              src={s.cover_url}
              alt={s.title}
              fill
              priority
              sizes="(max-width: 768px) 80vw, 280px"
              className="object-cover"
            />
          </div>
        </div>

        <div>
          <div className="flex items-center gap-2 mb-3">
            <span className="chip">{genreLabel(s.genre)}</span>
            <span className="chip-muted">{s.status}</span>
          </div>
          <h1 className="text-2xl md:text-3xl font-semibold tracking-tight">
            {s.title}
          </h1>
          <div className="mt-2 text-fg-muted">by {s.author}</div>
          <p className="mt-6 text-md text-fg-muted max-w-prose leading-relaxed">
            {s.description}
          </p>

          <div className="mt-8 flex items-center gap-3 flex-wrap">
            {firstEpisode && (
              <Link
                href={`/series/${s.id}/episode/${firstEpisode.episode_number}`}
                className="btn-md btn-primary gap-2"
              >
                <Play size={16} />
                Start reading · Ep {firstEpisode.episode_number}
              </Link>
            )}
            <BookmarkButton seriesID={s.id} />
          </div>

          <section className="mt-12">
            <div className="flex items-baseline justify-between mb-4">
              <h2 className="text-lg font-semibold">Episodes</h2>
              <span className="text-sm text-fg-subtle">{ep.total} total</span>
            </div>
            {eps.length === 0 ? (
              <div className="text-sm text-fg-muted">
                No episodes published yet.
              </div>
            ) : (
              <ul className="divide-y divide-border border border-border rounded-md overflow-hidden">
                {eps.map((e) => (
                  <li key={e.id}>
                    <Link
                      href={`/series/${s.id}/episode/${e.episode_number}`}
                      className="flex items-center gap-4 p-3 hover:bg-bg-hover transition-colors duration-120"
                    >
                      <div className="relative w-24 aspect-video rounded-sm overflow-hidden bg-bg flex-shrink-0 border border-border">
                        <Image
                          src={e.thumbnail_url}
                          alt=""
                          fill
                          sizes="96px"
                          className="object-cover"
                        />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="font-mono text-xs text-fg-subtle">
                            EP {String(e.episode_number).padStart(2, "0")}
                          </span>
                          <span className="font-medium truncate">{e.title}</span>
                        </div>
                        <div className="mt-1 flex items-center gap-3 text-xs text-fg-subtle">
                          <span className="flex items-center gap-1">
                            <Clock size={12} />
                            {new Date(e.created_at).toLocaleDateString()}
                          </span>
                        </div>
                      </div>
                      <Play size={16} className="text-fg-subtle" />
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}
