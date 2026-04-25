"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { admin, type AdminStats } from "@/lib/api";
import { ExternalLink, ListChecks, AlertTriangle, Loader2 } from "lucide-react";

// Jobs page. We show queue stats inline + an iframe embed of asynqmon
// (which docker-compose already runs on :8081). For production, put
// asynqmon behind an auth proxy; for local dev the iframe Just Works.
const ASYNQMON_URL = "http://localhost:8081";

export default function JobsPage() {
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
    const t = setInterval(load, 3000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  return (
    <div className="p-6 lg:p-8">
      <div className="label-eyebrow flex items-center gap-1">
        <ListChecks size={12} /> Admin
      </div>
      <div className="mt-1 flex items-center gap-3">
        <h1 className="text-2xl font-semibold flex-1">Jobs &amp; queues</h1>
        <Link
          href={ASYNQMON_URL}
          target="_blank"
          className="btn-sm btn-ghost gap-2"
        >
          Open asynqmon <ExternalLink size={12} />
        </Link>
      </div>

      {err && (
        <div className="card p-3 mt-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="mt-6 grid grid-cols-2 lg:grid-cols-4 gap-3">
        <QueueCard label="Pending" value={stats?.queue_pending} color="amber" />
        <QueueCard label="Active" value={stats?.queue_active} color="accent" />
        <QueueCard label="Retry" value={stats?.queue_retry} color="red" />
        <QueueCard label="Archived" value={stats?.queue_archived} color="muted" />
      </div>

      <div className="mt-6 card p-2 overflow-hidden">
        <iframe
          src={ASYNQMON_URL}
          title="asynqmon"
          className="w-full h-[70vh] rounded-md bg-white"
        />
      </div>
    </div>
  );
}

function QueueCard({
  label,
  value,
  color,
}: {
  label: string;
  value: number | undefined;
  color: "accent" | "amber" | "red" | "muted";
}) {
  const colorClass = {
    accent: "text-accent",
    amber: "text-amber-400",
    red: "text-destructive",
    muted: "text-fg-subtle",
  }[color];
  return (
    <div className="card p-4">
      <div className="label-eyebrow">{label}</div>
      <div className={`mt-2 text-2xl font-semibold tabular-nums ${colorClass}`}>
        {value === undefined ? <Loader2 className="animate-spin inline" size={18} /> : value.toLocaleString()}
      </div>
    </div>
  );
}
