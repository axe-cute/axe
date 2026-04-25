"use client";

import Link from "next/link";
import { use, useCallback, useEffect, useRef, useState } from "react";
import {
  admin,
  episodes as episodesApi,
  type AdminPage,
} from "@/lib/api";
import { dedupeBy } from "@/lib/utils";
import type { Episode } from "@/lib/types";
import {
  ArrowLeft,
  CheckCircle2,
  Loader2,
  Trash2,
  Upload,
  AlertTriangle,
  GripVertical,
  Save,
  Shield,
} from "lucide-react";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  rectSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

// How often we re-fetch page status after enqueueing transforms. Pages
// typically finish within 1-3s; if the worker is backed up it can take
// longer. We stop polling when everything is terminal (ready/failed).
const POLL_INTERVAL_MS = 1500;

type RowState =
  | { kind: "queued"; name: string; size: number }
  | { kind: "uploading"; name: string; progress: number }
  | { kind: "registered"; page: AdminPage }
  | { kind: "error"; name: string; message: string };

export default function AdminEpisodePage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id: episodeID } = use(params);
  const [ep, setEp] = useState<Episode | null>(null);
  const [pages, setPages] = useState<AdminPage[]>([]);
  const [rows, setRows] = useState<RowState[]>([]);
  const [uploading, setUploading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [reordering, setReordering] = useState(false);
  const [dirty, setDirty] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  // Refs mirror latest state so the polling interval can read them
  // without adding them to the effect dependency array (which would
  // recreate the interval on every state change and cause a storm).
  const pagesRef = useRef(pages);
  pagesRef.current = pages;
  const rowsRef = useRef(rows);
  rowsRef.current = rows;
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
  );

  // Initial load + polling while any page is non-terminal.
  useEffect(() => {
    let cancelled = false;

    async function refresh() {
      try {
        const [e, list] = await Promise.all([
          episodesApi.get(episodeID),
          admin.listPages(episodeID),
        ]);
        if (cancelled) return;
        setEp(e);
        // Don't clobber the user's in-progress reorder with server state.
        if (!dirtyRef.current) setPages(list.data);
      } catch (e: unknown) {
        if (!cancelled) setErr(e instanceof Error ? e.message : String(e));
      }
    }

    refresh();
    const t = setInterval(() => {
      const needsPoll =
        pagesRef.current.some((p) => p.status === "uploaded" || p.status === "processing") ||
        rowsRef.current.some((r) => r.kind === "uploading" || r.kind === "queued");
      if (needsPoll) refresh();
    }, POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [episodeID]);

  const onFiles = useCallback(
    async (files: FileList | null) => {
      if (!files || files.length === 0) return;
      setErr(null);
      setUploading(true);

      // Seed UI rows so the user sees immediate feedback.
      const selected = Array.from(files);
      setRows((r) => [
        ...r,
        ...selected.map<RowState>((f) => ({ kind: "queued", name: f.name, size: f.size })),
      ]);

      // Collect successful uploads to register in one batch at the end —
      // keeps DB insertion atomic and the transform queue dense.
      const registered: Array<{ key: string; content_type: string; bytes: number }> = [];

      for (let i = 0; i < selected.length; i++) {
        const f = selected[i];
        const rowIdx = rows.length + i;
        try {
          setRows((rs) => update(rs, rowIdx, { kind: "uploading", name: f.name, progress: 0 }));

          const presign = await admin.presignUpload({
            episode_id: episodeID,
            filename: f.name,
            content_type: f.type || "application/octet-stream",
            size: f.size,
          });

          await admin.uploadTo(presign.put_url, f);

          registered.push({
            key: presign.key,
            content_type: presign.content_type,
            bytes: f.size,
          });
          setRows((rs) => update(rs, rowIdx, { kind: "uploading", name: f.name, progress: 100 }));
        } catch (e: unknown) {
          const message = e instanceof Error ? e.message : String(e);
          setRows((rs) => update(rs, rowIdx, { kind: "error", name: f.name, message }));
        }
      }

      if (registered.length > 0) {
        try {
          const res = await admin.registerPages(episodeID, registered);
          // Remove the transient uploading rows — the server has now
          // persisted them and the polling useEffect will pick them up
          // as real pages (status='uploaded' → 'ready').
          setRows((rs) => rs.filter((r) => r.kind !== "uploading"));
          // Append new pages to the table so the user sees them transition
          // uploaded → processing → ready without waiting for the next poll.
          setPages((p) => [...p, ...res.data]);
        } catch (e: unknown) {
          setErr(e instanceof Error ? e.message : String(e));
        }
      }

      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = "";
    },
    [episodeID, rows.length],
  );

  async function onDelete(pageID: string) {
    if (!confirm("Delete this page? Storage objects are removed best-effort.")) return;
    try {
      await admin.deletePage(pageID);
      setPages((p) => p.filter((x) => x.id !== pageID));
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  }

  function onDragEnd(event: DragEndEvent) {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setPages((xs) => {
      const oldIdx = xs.findIndex((p) => p.id === active.id);
      const newIdx = xs.findIndex((p) => p.id === over.id);
      if (oldIdx < 0 || newIdx < 0) return xs;
      return arrayMove(xs, oldIdx, newIdx);
    });
    setDirty(true);
  }

  async function saveOrder() {
    try {
      setReordering(true);
      setErr(null);
      await admin.reorderPages(
        episodeID,
        pages.map((p) => p.id),
      );
      // Refresh to pick up server-side page_num values.
      const list = await admin.listPages(episodeID);
      setPages(list.data);
      setDirty(false);
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setReordering(false);
    }
  }

  async function discardOrder() {
    setDirty(false);
    const list = await admin.listPages(episodeID);
    setPages(list.data);
  }

  return (
    <div className="p-6 lg:p-8">
      <Link href={ep ? `/admin/series/${ep.series_id ?? ""}` : "/admin"} className="btn-sm btn-ghost gap-2 mb-4">
        <ArrowLeft size={14} /> Back
      </Link>

      <div className="label-eyebrow flex items-center gap-1">
        <Shield size={12} /> Admin · episode
      </div>
      <h1 className="mt-1 text-2xl font-semibold">
        {ep ? `EP ${ep.episode_number} · ${ep.title}` : episodeID}
      </h1>
      <p className="mt-1 text-sm text-fg-subtle font-mono">{episodeID}</p>

      {err && (
        <div className="card mt-4 p-3 border-destructive/40 bg-destructive/10 text-destructive text-sm flex items-start gap-2">
          <AlertTriangle size={16} className="mt-0.5 flex-shrink-0" />
          <span>{err}</span>
        </div>
      )}

      <section className="mt-8">
        <h2 className="text-sm font-medium uppercase tracking-wider text-fg-subtle mb-3">
          Upload pages
        </h2>
        <label
          className="card flex flex-col items-center justify-center p-10 border-2 border-dashed border-border hover:border-accent/60 cursor-pointer transition-colors"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            onFiles(e.dataTransfer.files);
          }}
        >
          <Upload size={20} className="text-fg-subtle mb-2" />
          <div className="text-sm">
            {uploading ? "Uploading…" : "Drop images or click to select"}
          </div>
          <div className="text-xs text-fg-subtle mt-1">
            JPEG / PNG / WebP · up to 20 MB each
          </div>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            accept="image/jpeg,image/png,image/webp"
            className="hidden"
            onChange={(e) => onFiles(e.target.files)}
            disabled={uploading}
          />
        </label>

        {rows.length > 0 && (
          <ul className="mt-3 text-xs divide-y divide-border border border-border rounded-md overflow-hidden">
            {rows.map((r, i) => (
              <li key={`${r.kind}-${i}`} className="px-3 py-2 flex items-center gap-3">
                <span className="flex-1 truncate font-mono">
                  {"name" in r ? r.name : ""}
                </span>
                {r.kind === "uploading" && (
                  <span className="text-fg-subtle flex items-center gap-1">
                    <Loader2 size={12} className="animate-spin" /> {r.progress}%
                  </span>
                )}
                {r.kind === "error" && (
                  <span className="text-destructive">{r.message}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="mt-10">
        <div className="flex items-center gap-3 mb-3">
          <h2 className="text-sm font-medium uppercase tracking-wider text-fg-subtle flex-1">
            Pages ({pages.length})
          </h2>
          {dirty && (
            <>
              <span className="text-xs text-amber-500 font-medium">
                Unsaved order
              </span>
              <button
                onClick={discardOrder}
                className="btn-sm btn-ghost"
                disabled={reordering}
              >
                Discard
              </button>
              <button
                onClick={saveOrder}
                className="btn-sm btn-primary gap-2"
                disabled={reordering}
              >
                {reordering ? (
                  <Loader2 size={12} className="animate-spin" />
                ) : (
                  <Save size={12} />
                )}
                Save order
              </button>
            </>
          )}
        </div>
        {pages.length === 0 ? (
          <p className="text-sm text-fg-muted">No pages uploaded.</p>
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={onDragEnd}
          >
            <SortableContext
              items={dedupeBy(pages, (p) => p.id).map((p) => p.id)}
              strategy={rectSortingStrategy}
            >
              <div className="grid grid-cols-2 sm:grid-cols-4 md:grid-cols-6 gap-3">
                {dedupeBy(pages, (p) => p.id).map((p, i) => (
                  <SortablePageCard
                    key={p.id}
                    page={p}
                    displayNum={i + 1}
                    onDelete={() => onDelete(p.id)}
                  />
                ))}
              </div>
            </SortableContext>
          </DndContext>
        )}
      </section>
    </div>
  );
}

function SortablePageCard({
  page: p,
  displayNum,
  onDelete,
}: {
  page: AdminPage;
  displayNum: number;
  onDelete: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: p.id });
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : 1,
  };
  return (
    <div
      ref={setNodeRef}
      style={style}
      className="relative aspect-[3/4] rounded-md overflow-hidden border border-border bg-bg-elevated group"
    >
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={p.thumb_url || p.original_url}
        alt={`Page ${p.page_num}`}
        className="h-full w-full object-cover pointer-events-none select-none"
      />
      <div
        {...attributes}
        {...listeners}
        className="absolute inset-0 cursor-grab active:cursor-grabbing"
        aria-label={`Drag page ${p.page_num}`}
      />
      <div className="absolute top-1 left-1 chip bg-bg/80 backdrop-blur text-[10px] flex items-center gap-1">
        <GripVertical size={10} />
        {String(displayNum).padStart(2, "0")}
      </div>
      <div className="absolute bottom-1 right-1">
        <StatusPill status={p.status} />
      </div>
      <button
        onClick={onDelete}
        className="absolute top-1 right-1 w-7 h-7 rounded-full bg-bg/80 backdrop-blur hover:bg-destructive hover:text-white flex items-center justify-center transition-colors opacity-0 group-hover:opacity-100"
        title="Delete page"
      >
        <Trash2 size={12} />
      </button>
    </div>
  );
}

function StatusPill({ status }: { status: AdminPage["status"] }) {
  switch (status) {
    case "ready":
      return (
        <span className="chip bg-emerald-500/20 text-emerald-600 border-emerald-500/40 text-[10px] flex items-center gap-1">
          <CheckCircle2 size={10} /> ready
        </span>
      );
    case "processing":
    case "uploaded":
      return (
        <span className="chip bg-amber-500/20 text-amber-600 border-amber-500/40 text-[10px] flex items-center gap-1">
          <Loader2 size={10} className="animate-spin" /> {status}
        </span>
      );
    case "failed":
      return (
        <span className="chip bg-destructive/20 text-destructive border-destructive/40 text-[10px]">
          failed
        </span>
      );
  }
}

function update<T>(arr: T[], idx: number, val: T): T[] {
  const copy = arr.slice();
  copy[idx] = val;
  return copy;
}
