"use client";

import { useEffect, useState } from "react";
import { admin, type UserRow } from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import { AlertTriangle, Info, Users } from "lucide-react";

export default function UsersPage() {
  const [users, setUsers] = useState<UserRow[]>([]);
  const [note, setNote] = useState<string | undefined>();
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    admin
      .users()
      .then((r) => {
        setUsers(r.data);
        setNote(r.note);
      })
      .catch((e) => setErr(e.message));
  }, []);

  return (
    <div className="p-6 lg:p-8">
      <div className="label-eyebrow flex items-center gap-1">
        <Users size={12} /> Admin
      </div>
      <h1 className="mt-1 text-2xl font-semibold">Users ({users.length})</h1>

      {note && (
        <div className="card mt-4 p-3 border-amber-500/30 bg-amber-500/10 text-amber-600 text-sm flex items-start gap-2">
          <Info size={14} className="mt-0.5" /> {note}
        </div>
      )}
      {err && (
        <div className="card mt-4 p-3 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="mt-6 overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-xs uppercase tracking-wider text-fg-subtle border-b border-border">
            <tr>
              <th className="text-left py-2 font-medium">User ID</th>
              <th className="text-right py-2 font-medium">Bookmarks</th>
              <th className="text-left py-2 font-medium pl-6">First seen</th>
              <th className="text-left py-2 font-medium pl-6">Last activity</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            {dedupeBy(users, (u) => u.user_id).map((u) => (
              <tr key={u.user_id} className="hover:bg-bg-hover transition-colors">
                <td className="py-2 font-mono text-xs truncate max-w-[260px]">
                  {u.user_id}
                </td>
                <td className="py-2 text-right tabular-nums">{u.bookmarks}</td>
                <td className="py-2 pl-6 text-fg-muted">
                  {u.first_seen.slice(0, 10)}
                </td>
                <td className="py-2 pl-6 text-fg-muted">
                  {u.last_activity.replace("T", " ")}
                </td>
              </tr>
            ))}
            {users.length === 0 && !err && (
              <tr>
                <td colSpan={4} className="py-8 text-center text-fg-muted">
                  No user activity recorded yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
