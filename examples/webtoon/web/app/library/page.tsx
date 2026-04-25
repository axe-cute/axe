"use client";
import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  bookmarks as bookmarksApi,
  series as seriesApi,
  getToken,
  getUser,
} from "@/lib/api";
import { SeriesCard } from "@/components/SeriesCard";
import type { Series } from "@/lib/types";
import { BookmarkX } from "lucide-react";

export default function LibraryPage() {
  const router = useRouter();
  const [items, setItems] = useState<Series[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!getToken()) {
      router.push("/auth/login?next=/library");
      return;
    }
    (async () => {
      try {
        const { data: bms } = await bookmarksApi.listMine();
        const results: Series[] = [];
        await Promise.all(
          bms.map(async (b) => {
            try {
              results.push(await seriesApi.get(b.series_id));
            } catch {
              /* skipped */
            }
          })
        );
        setItems(results);
      } finally {
        setLoading(false);
      }
    })();
  }, [router]);

  const me = getUser();

  return (
    <div className="container-gutter py-10">
      <div className="mb-8">
        <div className="label-eyebrow">Library</div>
        <h1 className="mt-2 text-xl font-semibold">
          {me ? `${me.email}'s bookmarks` : "Your bookmarks"}
        </h1>
      </div>

      {loading ? (
        <Skeleton />
      ) : items.length === 0 ? (
        <Empty />
      ) : (
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
          {items.map((s) => (
            <SeriesCard key={s.id} s={s} />
          ))}
        </div>
      )}
    </div>
  );
}

function Skeleton() {
  return (
    <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 gap-4">
      {Array.from({ length: 5 }).map((_, i) => (
        <div
          key={i}
          className="aspect-[3/4] rounded-md bg-bg-elevated border border-border animate-pulse"
        />
      ))}
    </div>
  );
}

function Empty() {
  return (
    <div className="text-center py-24 max-w-md mx-auto">
      <div className="mx-auto h-12 w-12 rounded-full bg-bg-elevated border border-border flex items-center justify-center text-fg-subtle">
        <BookmarkX size={24} />
      </div>
      <h2 className="mt-4 text-lg font-semibold">No bookmarks yet</h2>
      <p className="mt-2 text-fg-muted">
        Tap <span className="text-fg">Add to library</span> on any series to save
        it here.
      </p>
      <Link href="/browse" className="mt-6 inline-flex btn-md btn-primary">
        Browse catalog
      </Link>
    </div>
  );
}
