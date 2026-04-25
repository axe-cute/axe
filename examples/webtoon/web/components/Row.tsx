import { SeriesCard } from "./SeriesCard";
import type { Series } from "@/lib/types";
import Link from "next/link";
import { ArrowRight } from "lucide-react";

export function Row({
  title,
  series,
  href,
}: {
  title: string;
  series: Series[];
  href?: string;
}) {
  if (series.length === 0) return null;
  return (
    <section className="container-gutter py-8">
      <div className="flex items-end justify-between mb-4">
        <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
        {href && (
          <Link
            href={href}
            className="text-sm text-fg-muted hover:text-fg inline-flex items-center gap-1"
          >
            See all <ArrowRight size={14} />
          </Link>
        )}
      </div>
      <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
        {series.map((s) => (
          <SeriesCard key={s.id} s={s} />
        ))}
      </div>
    </section>
  );
}
