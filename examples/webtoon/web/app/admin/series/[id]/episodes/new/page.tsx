"use client";

import Link from "next/link";
import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { admin, episodes as episodesApi } from "@/lib/api";
import { AlertTriangle, ArrowLeft, Save } from "lucide-react";

export default function NewEpisodePage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id: seriesID } = use(params);
  const router = useRouter();
  const [title, setTitle] = useState("");
  const [epNum, setEpNum] = useState<number>(1);
  const [thumb, setThumb] = useState(
    `https://picsum.photos/seed/ep-${Date.now()}/400/240`,
  );
  const [published, setPublished] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Prefill episode_number = max + 1 so admins don't have to look it up.
  useEffect(() => {
    episodesApi
      .bySeries(seriesID, { limit: 200 })
      .then((r) => {
        const max = r.data.reduce(
          (acc, e) => Math.max(acc, e.episode_number),
          0,
        );
        setEpNum(max + 1);
      })
      .catch(() => {});
  }, [seriesID]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setErr(null);
    try {
      const ep = await admin.createEpisode({
        series_id: seriesID,
        title: title.trim(),
        episode_number: epNum,
        thumbnail_url: thumb.trim(),
        published,
      });
      router.push(`/admin/episodes/${ep.id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="p-6 lg:p-8 max-w-2xl">
      <Link href={`/admin/series/${seriesID}`} className="btn-sm btn-ghost gap-2 mb-4">
        <ArrowLeft size={14} /> Back to series
      </Link>
      <div className="label-eyebrow">Admin</div>
      <h1 className="mt-1 text-2xl font-semibold">New episode</h1>

      {err && (
        <div className="card p-3 mt-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="space-y-4 mt-6">
        <label className="block">
          <span className="label-eyebrow block mb-1.5">Title</span>
          <input
            className="input"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            minLength={2}
          />
        </label>

        <div className="grid grid-cols-[120px_1fr] gap-4">
          <label className="block">
            <span className="label-eyebrow block mb-1.5">Number</span>
            <input
              type="number"
              min={1}
              className="input"
              value={epNum}
              onChange={(e) => setEpNum(parseInt(e.target.value, 10) || 1)}
              required
            />
          </label>
          <label className="block">
            <span className="label-eyebrow block mb-1.5">Thumbnail URL</span>
            <input
              type="url"
              className="input"
              value={thumb}
              onChange={(e) => setThumb(e.target.value)}
              required
            />
          </label>
        </div>

        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={published}
            onChange={(e) => setPublished(e.target.checked)}
          />
          <span className="text-sm">Publish immediately</span>
        </label>
      </div>

      <div className="mt-6 flex items-center gap-3">
        <button type="submit" disabled={saving} className="btn-md btn-primary gap-2">
          <Save size={14} />
          {saving ? "Creating…" : "Create episode"}
        </button>
        <Link href={`/admin/series/${seriesID}`} className="btn-md btn-ghost">
          Cancel
        </Link>
      </div>
    </form>
  );
}
