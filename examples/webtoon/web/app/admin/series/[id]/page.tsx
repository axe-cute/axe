"use client";

import Link from "next/link";
import { use, useEffect, useState } from "react";
import {
  series as seriesApi,
  episodes as episodesApi,
  admin,
} from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import type { Series, Episode } from "@/lib/types";
import {
  ArrowLeft,
  ArrowRight,
  AlertTriangle,
  Edit3,
  Plus,
  Trash2,
} from "lucide-react";

export default function AdminSeriesPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const [s, setSeries] = useState<Series | null>(null);
  const [eps, setEps] = useState<Episode[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  async function reload() {
    try {
      const [sr, ep] = await Promise.all([
        seriesApi.get(id),
        episodesApi.bySeries(id, { limit: 200 }),
      ]);
      setSeries(sr);
      setEps([...ep.data].sort((a, b) => a.episode_number - b.episode_number));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  useEffect(() => {
    reload();
  }, [id]);

  async function deleteEpisode(epId: string) {
    if (!confirm("Delete this episode and all its pages?")) return;
    try {
      setBusy(epId);
      await admin.deleteEpisode(epId);
      setEps((xs) => xs.filter((x) => x.id !== epId));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  async function togglePublish(ep: Episode) {
    try {
      setBusy(ep.id);
      const updated = await admin.updateEpisode(ep.id, {
        ...ep,
        published: !ep.published,
      });
      setEps((xs) => xs.map((x) => (x.id === ep.id ? { ...x, ...updated } : x)));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="p-6 lg:p-8">
      <Link href="/admin/series" className="btn-sm btn-ghost gap-2 mb-4">
        <ArrowLeft size={14} /> All series
      </Link>

      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="label-eyebrow">Admin · series</div>
          <h1 className="mt-1 text-2xl font-semibold truncate">{s?.title ?? id}</h1>
          <p className="mt-1 text-sm text-fg-subtle font-mono truncate">{id}</p>
        </div>
        <Link href={`/admin/series/${id}/edit`} className="btn-sm btn-ghost gap-2">
          <Edit3 size={14} /> Edit metadata
        </Link>
      </div>

      {err && (
        <div className="card p-3 mt-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="mt-8 flex items-center gap-3">
        <h2 className="text-lg font-semibold flex-1">Episodes ({eps.length})</h2>
        <Link
          href={`/admin/series/${id}/episodes/new`}
          className="btn-md btn-primary gap-2"
        >
          <Plus size={14} /> New episode
        </Link>
      </div>

      <ul className="mt-3 divide-y divide-border border border-border rounded-md overflow-hidden">
        {dedupeBy(eps, (e) => e.id).map((e) => (
          <li
            key={e.id}
            className="flex items-center gap-3 p-3 hover:bg-bg-hover transition-colors"
          >
            <span className="font-mono text-xs text-fg-subtle min-w-[3rem]">
              EP {String(e.episode_number).padStart(2, "0")}
            </span>
            <Link
              href={`/admin/episodes/${e.id}`}
              className="min-w-0 flex-1 truncate font-medium hover:text-accent"
            >
              {e.title}
            </Link>
            <button
              onClick={() => togglePublish(e)}
              disabled={busy === e.id}
              className={`chip text-[11px] ${
                e.published
                  ? "bg-emerald-500/15 text-emerald-600 border-emerald-500/40"
                  : "bg-bg-elevated text-fg-muted border-border"
              }`}
            >
              {e.published ? "published" : "draft"}
            </button>
            <Link
              href={`/admin/episodes/${e.id}`}
              className="btn-sm btn-ghost"
              title="Open"
            >
              <ArrowRight size={14} />
            </Link>
            <button
              onClick={() => deleteEpisode(e.id)}
              disabled={busy === e.id}
              className="btn-sm btn-ghost text-destructive hover:bg-destructive/10"
              title="Delete"
            >
              <Trash2 size={14} />
            </button>
          </li>
        ))}
        {eps.length === 0 && (
          <li className="p-6 text-center text-sm text-fg-muted">
            No episodes yet.
          </li>
        )}
      </ul>
    </div>
  );
}
