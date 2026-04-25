"use client";

import { useEffect, useState } from "react";
import { admin, type AuditEntry } from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import { History, AlertTriangle, Loader2, Search } from "lucide-react";

const ACTION_FILTERS = [
  { label: "All", value: "" },
  { label: "Pages", value: "pages." },
  { label: "Uploads", value: "uploads." },
  { label: "Series", value: "series." },
  { label: "Episodes", value: "episodes." },
];

export default function AuditPage() {
  const [rows, setRows] = useState<AuditEntry[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [action, setAction] = useState("");
  const [q, setQ] = useState("");

  useEffect(() => {
    setLoading(true);
    admin
      .audit({ action: action || undefined, limit: 100 })
      .then((r) => setRows(r.data))
      .catch((e) => setErr(e.message))
      .finally(() => setLoading(false));
  }, [action]);

  const filtered = q
    ? rows.filter((r) =>
        `${r.action} ${r.subject_id ?? ""} ${r.actor_id ?? ""}`
          .toLowerCase()
          .includes(q.toLowerCase()),
      )
    : rows;

  return (
    <div className="p-6 lg:p-8">
      <div className="label-eyebrow flex items-center gap-1">
        <History size={12} /> Admin
      </div>
      <h1 className="mt-1 text-2xl font-semibold">Audit log</h1>

      {err && (
        <div className="card p-3 mt-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="mt-6 flex flex-wrap items-center gap-2">
        {ACTION_FILTERS.map((f) => (
          <button
            key={f.value}
            onClick={() => setAction(f.value)}
            className={`chip text-[11px] ${
              action === f.value
                ? "bg-accent-subtle text-accent border-accent/40"
                : "bg-bg-elevated text-fg-muted border-border"
            }`}
          >
            {f.label}
          </button>
        ))}
        <div className="relative flex-1 max-w-sm ml-auto">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-fg-subtle" />
          <input
            className="input pl-9"
            placeholder="Filter by id or action…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
      </div>

      <div className="mt-4 overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-xs uppercase tracking-wider text-fg-subtle border-b border-border">
            <tr>
              <th className="text-left py-2 font-medium">When</th>
              <th className="text-left py-2 font-medium pl-4">Action</th>
              <th className="text-left py-2 font-medium pl-4">Subject</th>
              <th className="text-left py-2 font-medium pl-4">Actor</th>
              <th className="text-right py-2 font-medium pl-4">HTTP</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {dedupeBy(filtered, (r) => r.id).map((r) => (
              <tr key={r.id} className="hover:bg-bg-hover transition-colors">
                <td className="py-2 text-fg-muted whitespace-nowrap">
                  {r.created_at.replace("T", " ").slice(0, 19)}
                </td>
                <td className="py-2 pl-4">
                  <span className="font-mono text-xs">{r.action}</span>
                </td>
                <td className="py-2 pl-4">
                  {r.subject_type && r.subject_id ? (
                    <span className="font-mono text-xs text-fg-subtle">
                      {r.subject_type}:{r.subject_id.slice(0, 8)}
                    </span>
                  ) : (
                    <span className="text-fg-subtle">—</span>
                  )}
                </td>
                <td className="py-2 pl-4 font-mono text-xs text-fg-subtle">
                  {r.actor_id ? r.actor_id.slice(0, 8) : "—"}
                </td>
                <td className="py-2 pl-4 text-right">
                  <StatusBadge status={r.status} />
                </td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={5} className="py-10 text-center text-fg-muted">
                  {loading ? (
                    <Loader2 className="animate-spin inline" size={16} />
                  ) : (
                    "No audit entries."
                  )}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: number }) {
  const color =
    status >= 500
      ? "text-destructive bg-destructive/10 border-destructive/40"
      : status >= 400
      ? "text-amber-500 bg-amber-500/10 border-amber-500/40"
      : "text-emerald-500 bg-emerald-500/10 border-emerald-500/40";
  return (
    <span className={`chip text-[10px] font-mono ${color}`}>{status}</span>
  );
}
