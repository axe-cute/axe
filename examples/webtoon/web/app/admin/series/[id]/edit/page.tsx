"use client";

import { use, useEffect, useState } from "react";
import { series as seriesApi } from "@/lib/api";
import type { Series } from "@/lib/types";
import SeriesForm from "@/components/SeriesForm";
import { Loader2 } from "lucide-react";

export default function EditSeriesPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const [s, setS] = useState<Series | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    seriesApi
      .get(id)
      .then(setS)
      .catch((e) => setErr(e.message));
  }, [id]);

  if (err) {
    return (
      <div className="p-8 text-destructive text-sm">Error: {err}</div>
    );
  }
  if (!s) {
    return (
      <div className="p-8 flex items-center gap-2 text-fg-subtle text-sm">
        <Loader2 size={14} className="animate-spin" /> Loading…
      </div>
    );
  }
  return <SeriesForm initial={s} />;
}
