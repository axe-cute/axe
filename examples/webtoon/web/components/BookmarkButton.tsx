"use client";
import { Bookmark as BookmarkIcon, BookmarkCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { bookmarks, getToken, getUser } from "@/lib/api";
import clsx from "clsx";

type Props = { seriesID: string; initial?: boolean };

export function BookmarkButton({ seriesID, initial = false }: Props) {
  const router = useRouter();
  const [on, setOn] = useState(initial);
  const [loading, setLoading] = useState(false);

  // If we have an auth session, fetch initial state from server.
  useEffect(() => {
    if (!getToken()) return;
    bookmarks
      .listMine()
      .then((res) => {
        setOn(res.data.some((b) => b.series_id === seriesID));
      })
      .catch(() => {});
  }, [seriesID]);

  async function toggle() {
    if (!getUser()) {
      router.push(`/auth/login?next=${encodeURIComponent(location.pathname)}`);
      return;
    }
    setLoading(true);
    try {
      const res = await bookmarks.toggle(seriesID);
      setOn(res.bookmarked);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }

  return (
    <button
      onClick={toggle}
      disabled={loading}
      aria-pressed={on}
      className={clsx(
        "btn-md gap-2",
        on ? "btn-secondary text-accent" : "btn-secondary"
      )}
    >
      {on ? <BookmarkCheck size={16} /> : <BookmarkIcon size={16} />}
      <span>{on ? "In library" : "Add to library"}</span>
    </button>
  );
}
