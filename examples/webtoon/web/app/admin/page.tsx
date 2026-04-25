"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { admin, type AdminStats } from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import {
  BookOpen,
  FileImage,
  Users,
  HardDrive,
  ListChecks,
  Flame,
  AlertTriangle,
  CheckCircle2,
  Loader2,
  TrendingUp,
  BookMarked,
} from "lucide-react";
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts";

// The dashboard polls /admin/stats every 5s so queue + transform progress
// stay visible. The endpoint is a single multi-subquery round-trip (see
// admin_dashboard.go), so this is cheap.
const POLL_MS = 5000;

export default function AdminDashboard() {
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const s = await admin.stats();
        if (!cancelled) setStats(s);
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : String(e));
      }
    }
    load();
    const t = setInterval(load, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  return (
    <div className="p-6 lg:p-8">
      <header className="mb-6 flex items-end justify-between">
        <div>
          <div className="label-eyebrow">Admin</div>
          <h1 className="mt-1 text-2xl font-semibold">Dashboard</h1>
        </div>
        {stats && (
          <div className="text-xs text-fg-subtle font-mono">
            Auto-refresh · {POLL_MS / 1000}s
          </div>
        )}
      </header>

      {err && (
        <div className="card p-3 mb-6 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <section className="grid grid-cols-2 lg:grid-cols-4 gap-3">
        <StatCard
          icon={BookOpen}
          label="Series"
          value={stats?.series}
          sub={stats ? `${stats.series_ongoing} ongoing · ${stats.series_completed} completed` : null}
        />
        <StatCard
          icon={FileImage}
          label="Episodes"
          value={stats?.episodes}
          sub={stats ? `${stats.episodes_published} published` : null}
        />
        <StatCard
          icon={FileImage}
          label="Pages"
          value={stats?.pages}
          sub={
            stats ? (
              <span>
                <PageStatusPill n={stats.pages_ready} color="emerald" label="ready" />{" "}
                <PageStatusPill n={stats.pages_pending} color="amber" label="pending" />{" "}
                <PageStatusPill n={stats.pages_failed} color="red" label="failed" />
              </span>
            ) : null
          }
        />
        <StatCard
          icon={HardDrive}
          label="Storage"
          value={stats ? formatBytes(stats.storage_bytes) : undefined}
          sub={stats ? `${stats.pages} pages on disk` : null}
          raw
        />
        <StatCard
          icon={Users}
          label="Readers"
          value={stats?.distinct_users}
          sub={stats ? `${stats.bookmarks} bookmarks total` : null}
        />
        <StatCard
          icon={ListChecks}
          label="Queue depth"
          value={stats?.queue_depth}
          sub={
            stats ? (
              <span>
                pending {stats.queue_pending} · active {stats.queue_active} · retry {stats.queue_retry}
              </span>
            ) : null
          }
        />
        <StatCard
          icon={BookMarked}
          label="Bookmarks"
          value={stats?.bookmarks}
          sub={stats ? `${stats.distinct_users} unique readers` : null}
        />
        <StatCard
          icon={Flame}
          label="Trending"
          value={stats?.trending.length}
          sub={stats ? "series with score > 0" : null}
        />
      </section>

      <section className="mt-8 grid lg:grid-cols-3 gap-4">
        <div className="card p-4 lg:col-span-2">
          <div className="flex items-center gap-2 mb-3">
            <TrendingUp size={14} className="text-fg-subtle" />
            <h2 className="text-sm font-medium">Uploads · last 14 days</h2>
          </div>
          <div className="h-56">
            {stats ? (
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={stats.uploads_by_day}>
                  <CartesianGrid strokeDasharray="3 3" stroke="currentColor" opacity={0.08} />
                  <XAxis
                    dataKey="day"
                    tickFormatter={(d: string) => d.slice(5)}
                    stroke="currentColor"
                    fontSize={11}
                  />
                  <YAxis allowDecimals={false} stroke="currentColor" fontSize={11} />
                  <Tooltip
                    contentStyle={{
                      background: "var(--bg-elevated, #111)",
                      border: "1px solid var(--border, #333)",
                      borderRadius: 4,
                      fontSize: 12,
                    }}
                  />
                  <Bar dataKey="count" fill="currentColor" className="text-accent" />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <div className="h-full flex items-center justify-center text-fg-subtle">
                <Loader2 className="animate-spin" size={16} />
              </div>
            )}
          </div>
        </div>

        <div className="card p-4">
          <div className="flex items-center gap-2 mb-3">
            <Flame size={14} className="text-fg-subtle" />
            <h2 className="text-sm font-medium">Trending top 5</h2>
          </div>
          <ol className="divide-y divide-border text-sm">
            {dedupeBy(stats?.trending ?? [], (t) => t.id).map((t, i) => (
              <li key={t.id} className="py-2 flex items-center gap-2">
                <span className="font-mono text-xs text-fg-subtle w-5">
                  {i + 1}
                </span>
                <Link
                  href={`/admin/series/${t.id}`}
                  className="flex-1 truncate hover:text-accent"
                >
                  {t.title}
                </Link>
                <span className="font-mono text-xs text-fg-subtle">
                  {t.trending_score.toFixed(2)}
                </span>
              </li>
            ))}
            {stats && stats.trending.length === 0 && (
              <li className="py-2 text-fg-muted text-sm">No trending data yet.</li>
            )}
          </ol>
        </div>
      </section>

      <section className="mt-8">
        <div className="flex items-center gap-2 mb-3">
          <FileImage size={14} className="text-fg-subtle" />
          <h2 className="text-sm font-medium">Recent uploads</h2>
          <Link href="/admin/series" className="ml-auto text-xs text-accent hover:underline">
            All series →
          </Link>
        </div>
        {stats?.recent_uploads.length ? (
          <div className="grid grid-cols-4 md:grid-cols-8 gap-2">
            {dedupeBy(stats.recent_uploads, (p) => p.id).map((p) => (
              <div
                key={p.id}
                className="relative aspect-[3/4] rounded-md overflow-hidden border border-border bg-bg-elevated"
                title={`page ${p.page_num} · ${p.status}`}
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={p.thumb_url || p.original_url}
                  alt=""
                  className="h-full w-full object-cover"
                />
                <div className="absolute bottom-1 right-1">
                  {p.status === "ready" ? (
                    <CheckCircle2 size={12} className="text-emerald-400" />
                  ) : p.status === "failed" ? (
                    <AlertTriangle size={12} className="text-destructive" />
                  ) : (
                    <Loader2 size={12} className="animate-spin text-amber-400" />
                  )}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-fg-muted">No uploads yet.</p>
        )}
      </section>
    </div>
  );
}

function StatCard({
  icon: Icon,
  label,
  value,
  sub,
  raw,
}: {
  icon: React.ComponentType<{ size?: number; className?: string }>;
  label: string;
  value: number | string | undefined;
  sub?: React.ReactNode;
  raw?: boolean;
}) {
  return (
    <div className="card p-4">
      <div className="flex items-center gap-2 text-fg-subtle">
        <Icon size={14} />
        <span className="label-eyebrow">{label}</span>
      </div>
      <div className="mt-2 text-2xl font-semibold tabular-nums">
        {value === undefined ? (
          <span className="text-fg-subtle">—</span>
        ) : raw ? (
          value
        ) : (
          Number(value).toLocaleString()
        )}
      </div>
      {sub && <div className="mt-1 text-xs text-fg-subtle">{sub}</div>}
    </div>
  );
}

function PageStatusPill({
  n,
  color,
  label,
}: {
  n: number;
  color: "emerald" | "amber" | "red";
  label: string;
}) {
  const cls = {
    emerald: "text-emerald-400",
    amber: "text-amber-400",
    red: "text-destructive",
  }[color];
  return (
    <span className={cls}>
      {n} {label}
    </span>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
