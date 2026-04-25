"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { admin } from "@/lib/api";
import type { Series } from "@/lib/types";
import { GENRES, genreLabel } from "@/lib/genres";
import { AlertTriangle, Save, ArrowLeft } from "lucide-react";

// Reusable form for both create + edit. `initial` being null means create.
// We avoid a separate server action/route for each mode — the submit
// function is the only branch.
export default function SeriesForm({ initial }: { initial: Series | null }) {
  const router = useRouter();
  const [title, setTitle] = useState(initial?.title ?? "");
  const [author, setAuthor] = useState(initial?.author ?? "");
  const [genre, setGenre] = useState(initial?.genre ?? GENRES[0]);
  const [status, setStatus] = useState(initial?.status ?? "ongoing");
  const [cover, setCover] = useState(
    initial?.cover_url ?? "https://picsum.photos/seed/new/600/800",
  );
  const [description, setDescription] = useState(initial?.description ?? "");
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setErr(null);
    try {
      const data = {
        title: title.trim(),
        author: author.trim(),
        genre,
        status,
        cover_url: cover.trim(),
        description: description.trim(),
      };
      const saved = initial
        ? await admin.updateSeries(initial.id, data)
        : await admin.createSeries(data);
      router.push(`/admin/series/${saved.id}`);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={onSubmit} className="p-6 lg:p-8 max-w-3xl">
      <Link href="/admin/series" className="btn-sm btn-ghost gap-2 mb-4">
        <ArrowLeft size={14} /> Back
      </Link>
      <div className="label-eyebrow">Admin</div>
      <h1 className="mt-1 text-2xl font-semibold">
        {initial ? "Edit series" : "New series"}
      </h1>

      {err && (
        <div className="card p-3 mt-4 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-center gap-2">
          <AlertTriangle size={14} /> {err}
        </div>
      )}

      <div className="grid lg:grid-cols-[1fr_240px] gap-6 mt-6">
        <div className="space-y-4">
          <Field label="Title">
            <input
              className="input"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              required
              minLength={2}
            />
          </Field>

          <Field label="Author">
            <input
              className="input"
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              required
              minLength={1}
            />
          </Field>

          <div className="grid grid-cols-2 gap-4">
            <Field label="Genre">
              <select
                className="input"
                value={genre}
                onChange={(e) => setGenre(e.target.value)}
              >
                {GENRES.map((g) => (
                  <option key={g} value={g}>
                    {genreLabel(g)}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Status">
              <select
                className="input"
                value={status}
                onChange={(e) => setStatus(e.target.value)}
              >
                <option value="ongoing">ongoing</option>
                <option value="completed">completed</option>
                <option value="hiatus">hiatus</option>
              </select>
            </Field>
          </div>

          <Field label="Cover URL">
            <input
              type="url"
              className="input"
              value={cover}
              onChange={(e) => setCover(e.target.value)}
              required
            />
          </Field>

          <Field label="Description">
            <textarea
              className="input min-h-[120px]"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={5}
            />
          </Field>
        </div>

        <div>
          <div className="label-eyebrow mb-2">Preview</div>
          <div className="aspect-[3/4] rounded-md border border-border bg-bg-elevated overflow-hidden">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src={cover}
              alt="cover preview"
              className="h-full w-full object-cover"
            />
          </div>
        </div>
      </div>

      <div className="mt-6 flex items-center gap-3">
        <button type="submit" disabled={saving} className="btn-md btn-primary gap-2">
          <Save size={14} />
          {saving ? "Saving…" : initial ? "Save changes" : "Create series"}
        </button>
        <Link href="/admin/series" className="btn-md btn-ghost">
          Cancel
        </Link>
      </div>
    </form>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="label-eyebrow block mb-1.5">{label}</span>
      {children}
    </label>
  );
}
