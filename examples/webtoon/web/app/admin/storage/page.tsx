"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { admin, type AdminStats } from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import { HardDrive, ExternalLink, AlertTriangle, CheckCircle2, Loader2, XCircle } from "lucide-react";

const MINIO_CONSOLE = "http://localhost:9001";

export default function StoragePage() {
  const [stats, setStats] = useState<AdminStats | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    admin.stats().then(setStats).catch((e) => setErr(e.message));
  }, []);

  return (
    <div className="p-6 lg:p-8">
      <div className="label-eyebrow flex items-center gap-1">
        <HardDrive size={12} /> Admin
      </div>
      <div className="mt-1 flex items-center gap-3">
        <h1 className="text-2xl font-semibold flex-1">Storage</h1>
        <Link
          href={MINIO_CONSOLE}
          target="_blank"
          className="btn-sm btn-ghost gap-2"
        >
          MinIO console <ExternalLink size={12} />
        </Link>
      </div>

      {err && (
        <div className="card p-3 mt-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <section className="mt-6 grid grid-cols-2 lg:grid-cols-4 gap-3">
        <Card label="Total bytes" value={stats ? formatBytes(stats.storage_bytes) : "—"} />
        <Card label="Pages" value={stats?.pages.toLocaleString() ?? "—"} />
        <Card
          label="Ready"
          value={stats?.pages_ready.toLocaleString() ?? "—"}
          icon={CheckCircle2}
          color="text-emerald-500"
        />
        <Card
          label="Failed"
          value={stats?.pages_failed.toLocaleString() ?? "—"}
          icon={XCircle}
          color="text-destructive"
        />
      </section>

      <section className="mt-8 card p-4">
        <h2 className="text-sm font-medium mb-2">Cost projection</h2>
        <p className="text-sm text-fg-muted">
          At current storage size ({stats ? formatBytes(stats.storage_bytes) : "—"}):
        </p>
        <ul className="mt-3 space-y-1 text-sm">
          <ProjectionRow
            label="Backblaze B2"
            bytes={stats?.storage_bytes}
            perTbMonth={6}
            egressNote="free via Cloudflare Bandwidth Alliance"
          />
          <ProjectionRow
            label="Cloudflare R2"
            bytes={stats?.storage_bytes}
            perTbMonth={15}
            egressNote="free egress (always)"
          />
          <ProjectionRow
            label="AWS S3 Standard"
            bytes={stats?.storage_bytes}
            perTbMonth={23}
            egressNote="+$0.09/GB egress"
          />
        </ul>
      </section>

      <section className="mt-8">
        <h2 className="text-sm font-medium mb-2 flex items-center gap-2">
          <Loader2 size={14} className="text-fg-subtle" />
          Recent uploads
        </h2>
        {stats?.recent_uploads.length ? (
          <div className="grid grid-cols-4 md:grid-cols-8 gap-2">
            {dedupeBy(stats.recent_uploads, (p) => p.id).map((p) => (
              <div
                key={p.id}
                className="relative aspect-[3/4] rounded-md overflow-hidden border border-border bg-bg-elevated"
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={p.thumb_url || p.original_url}
                  alt=""
                  className="h-full w-full object-cover"
                />
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-fg-muted">No uploads.</p>
        )}
      </section>
    </div>
  );
}

function Card({
  label,
  value,
  icon: Icon,
  color,
}: {
  label: string;
  value: string;
  icon?: React.ComponentType<{ size?: number; className?: string }>;
  color?: string;
}) {
  return (
    <div className="card p-4">
      <div className="label-eyebrow flex items-center gap-1">
        {Icon && <Icon size={12} className={color} />}
        {label}
      </div>
      <div className={`mt-2 text-2xl font-semibold tabular-nums ${color ?? ""}`}>
        {value}
      </div>
    </div>
  );
}

function ProjectionRow({
  label,
  bytes,
  perTbMonth,
  egressNote,
}: {
  label: string;
  bytes?: number;
  perTbMonth: number;
  egressNote: string;
}) {
  const tb = (bytes ?? 0) / (1024 * 1024 * 1024 * 1024);
  const cost = tb * perTbMonth;
  return (
    <li className="flex items-center gap-3">
      <span className="w-40 text-fg-muted">{label}</span>
      <span className="font-mono tabular-nums">
        ${cost.toFixed(4)}/mo
      </span>
      <span className="text-xs text-fg-subtle ml-auto">{egressNote}</span>
    </li>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
