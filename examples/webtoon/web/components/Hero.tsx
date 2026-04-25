import Image from "next/image";
import Link from "next/link";
import type { Series } from "@/lib/types";
import { ArrowRight, Sparkles } from "lucide-react";
import { genreLabel } from "@/lib/genres";

export function Hero({ featured }: { featured: Series | null }) {
  return (
    <section className="relative overflow-hidden">
      <div
        aria-hidden
        className="absolute inset-0 pointer-events-none"
        style={{
          background:
            "radial-gradient(1000px 500px at 10% -10%, hsl(var(--accent-subtle) / 0.55), transparent 55%), radial-gradient(700px 400px at 85% 10%, hsl(var(--accent) / 0.12), transparent 60%)",
        }}
      />
      <div className="container-gutter relative py-16 md:py-24 lg:py-32 grid md:grid-cols-[1.1fr_1fr] gap-12 items-center">
        <div className="max-w-xl">
          <div className="inline-flex items-center gap-2 chip mb-6">
            <Sparkles size={12} />
            <span>New this week</span>
          </div>
          <h1 className="text-2xl md:text-3xl font-semibold tracking-tight leading-tight">
            Serialized stories, <span className="text-accent">read at your pace</span>.
          </h1>
          <p className="mt-4 text-lg text-fg-muted max-w-lg">
            A reader-first webtoon platform built on <span className="font-mono text-fg">axe</span> —
            Clean Architecture Go backend, Next.js frontend, zero runtime magic.
          </p>
          <div className="mt-8 flex items-center gap-3">
            <Link href="/browse" className="btn-lg btn-primary gap-2">
              Start reading <ArrowRight size={16} />
            </Link>
            {featured && (
              <Link
                href={`/series/${featured.id}`}
                className="btn-lg btn-secondary"
              >
                Featured: {featured.title}
              </Link>
            )}
          </div>
        </div>

        {featured && (
          <Link
            href={`/series/${featured.id}`}
            className="relative group mx-auto w-full max-w-[340px] md:max-w-[380px]"
          >
            <div className="relative aspect-[3/4] overflow-hidden rounded-lg border border-border bg-bg-elevated shadow-lg">
              <Image
                src={featured.cover_url}
                alt={featured.title}
                fill
                sizes="(max-width: 768px) 80vw, 380px"
                priority
                className="object-cover transition-transform duration-240 ease-out group-hover:scale-[1.02]"
              />
            </div>
            <div className="mt-4">
              <div className="label-eyebrow">Featured</div>
              <div className="mt-1 text-lg font-medium">{featured.title}</div>
              <div className="mt-1 text-sm text-fg-muted">
                {genreLabel(featured.genre)} · {featured.author}
              </div>
            </div>
          </Link>
        )}
      </div>
    </section>
  );
}
