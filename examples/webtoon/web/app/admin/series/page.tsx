"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { series as seriesApi, admin } from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import type { Series } from "@/lib/types";
import { Plus, Trash2, Search, AlertTriangle } from "lucide-react";
import { genreLabel } from "@/lib/genres";

export default function AdminSeriesList() {
  const [list, setList] = useState<Series[]>([]);
  const [q, setQ] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  async function reload() {
    try {
      const r = await seriesApi.list({ limit: 200 });
      setList(r.data);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }
  useEffect(() => {
    reload();
  }, []);

  const filtered = list.filter((s) =>
    q ? `${s.title} ${s.author}`.toLowerCase().includes(q.toLowerCase()) : true,
  );

  async function onDelete(id: string) {
    if (!confirm("Delete this series? This will cascade to all episodes.")) return;
    try {
      setBusy(id);
      await admin.deleteSeries(id);
      setList((xs) => xs.filter((x) => x.id !== id));
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="p-6 lg:p-8">
      <header className="mb-6 flex items-center gap-3">
        <div className="flex-1">
          <div className="label-eyebrow">Admin</div>
          <h1 className="mt-1 text-2xl font-semibold">Series ({list.length})</h1>
        </div>
        <Link href="/admin/series/new" className="btn-md btn-primary gap-2">
          <Plus size={14} /> New series
        </Link>
      </header>

      {err && (
        <div className="card p-3 mb-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="mb-4 relative max-w-md">
        <Search
          size={14}
          className="absolute left-3 top-1/2 -translate-y-1/2 text-fg-subtle"
        />
        <input
          className="input pl-9"
          placeholder="Search title or author…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>

      <ul className="divide-y divide-border border border-border rounded-md overflow-hidden">
        {dedupeBy(filtered, (s) => s.id).map((s) => (
          <li key={s.id} className="flex items-center gap-3 p-3 hover:bg-bg-hover transition-colors">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={s.cover_url}
              alt=""
              className="w-12 h-16 object-cover rounded border border-border flex-shrink-0"
            />
            <div className="min-w-0 flex-1">
              <Link
                href={`/admin/series/${s.id}`}
                className="font-medium truncate hover:text-accent block"
              >
                {s.title}
              </Link>
              <div className="text-xs text-fg-subtle truncate">
                {s.author} · {genreLabel(s.genre)} ·{" "}
                <span className="chip-muted">{s.status}</span>
              </div>
            </div>
            <Link
              href={`/admin/series/${s.id}/edit`}
              className="btn-sm btn-ghost"
            >
              Edit
            </Link>
            <button
              onClick={() => onDelete(s.id)}
              disabled={busy === s.id}
              className="btn-sm btn-ghost text-destructive hover:bg-destructive/10"
              title="Delete"
            >
              <Trash2 size={14} />
            </button>
          </li>
        ))}
        {filtered.length === 0 && (
          <li className="p-6 text-center text-sm text-fg-muted">
            {q ? "No matches." : "No series yet."}
          </li>
        )}
      </ul>
    </div>
  );
}
