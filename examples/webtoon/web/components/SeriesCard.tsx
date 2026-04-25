import Image from "next/image";
import Link from "next/link";
import type { Series } from "@/lib/types";
import { genreLabel } from "@/lib/genres";

export function SeriesCard({ s, priority = false }: { s: Series; priority?: boolean }) {
  return (
    <Link
      href={`/series/${s.id}`}
      className="group flex flex-col gap-2 text-left"
    >
      <div className="relative aspect-[3/4] overflow-hidden rounded-md border border-border bg-bg-elevated transition-all duration-180 ease-out group-hover:border-border-strong group-hover:shadow-md">
        {/*
          next/image emits an AVIF/WebP variant and correct srcset so phones
          don't download the full 600×800 cover. Covers are remote (Picsum
          in demo), so hosts are whitelisted in next.config.mjs.
        */}
        <Image
          src={s.cover_url}
          alt={s.title}
          fill
          sizes="(max-width: 640px) 45vw, (max-width: 1024px) 22vw, 18vw"
          className="object-cover transition-transform duration-240 ease-out group-hover:scale-[1.03]"
          priority={priority}
        />
        {s.status !== "ongoing" && (
          <span className="absolute top-2 left-2 chip-muted bg-bg/80 backdrop-blur">
            {s.status}
          </span>
        )}
      </div>
      <div className="min-w-0">
        <div className="text-md font-medium text-fg truncate group-hover:text-accent transition-colors duration-120">
          {s.title}
        </div>
        <div className="mt-1 flex items-center gap-2 text-xs text-fg-muted">
          <span className="chip">{genreLabel(s.genre)}</span>
          <span className="truncate">{s.author}</span>
        </div>
      </div>
    </Link>
  );
}
