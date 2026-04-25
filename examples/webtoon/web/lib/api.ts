import type {
  ApiError,
  Bookmark,
  Episode,
  ListEnvelope,
  LoginResponse,
  Series,
  ToggleResult,
} from "./types";

// When running in the browser, requests go to /api/* which Next rewrites to the Go API.
// When running server-side (RSC/fetch), prefer a direct URL (NEXT_INTERNAL_API_URL for in-cluster DNS).
const SERVER_BASE =
  process.env.NEXT_INTERNAL_API_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

function base(): string {
  if (typeof window === "undefined") return SERVER_BASE;
  return ""; // same-origin — hits /api/... rewrites
}

export const TOKEN_KEY = "webtoon:token";
export const USER_KEY = "webtoon:user";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}
export function setSession(resp: LoginResponse) {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(TOKEN_KEY, resp.access_token);
  window.localStorage.setItem(
    USER_KEY,
    JSON.stringify({ id: resp.user_id, email: resp.email, role: resp.role })
  );
  window.dispatchEvent(new Event("webtoon:auth"));
}
export function clearSession() {
  if (typeof window === "undefined") return;
  window.localStorage.removeItem(TOKEN_KEY);
  window.localStorage.removeItem(USER_KEY);
  window.dispatchEvent(new Event("webtoon:auth"));
}
export function getUser(): { id: string; email: string; role: string } | null {
  if (typeof window === "undefined") return null;
  const raw = window.localStorage.getItem(USER_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

// ReqInit extends RequestInit with Next's fetch cache hints so callers can
// opt into ISR on the server. If `next.revalidate` is set, we do NOT force
// `cache: "no-store"` — that lets Next memo the response for N seconds per
// URL+header combination. Client-side mutations always run uncached.
type ReqInit = RequestInit & { next?: { revalidate?: number; tags?: string[] } };

async function req<T>(path: string, init: ReqInit = {}): Promise<T> {
  const url = `${base()}/api/v1${path}`;
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type") && init.body) {
    headers.set("Content-Type", "application/json");
  }
  const token = getToken();
  if (token && !headers.has("Authorization")) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const fetchOpts: ReqInit = { ...init, headers };
  // Default to no-store so browser-side mutations and auth calls aren't
  // memoised. Server pages that want ISR explicitly pass `next.revalidate`.
  if (!init.next) {
    (fetchOpts as RequestInit).cache = "no-store";
  }

  const res = await fetch(url, fetchOpts);
  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const parsed = text ? safeJson(text) : null;

  if (!res.ok) {
    const err = (parsed as ApiError) || {
      code: "http_error",
      message: `${res.status} ${res.statusText}`,
    };
    // 401 on an *authenticated* request (we sent a token) almost always
    // means the JWT expired or was revoked. Clear the stale session so
    // Navbar / guarded pages re-render as signed-out, and bounce to login
    // with a return-to so the user lands back where they were. We skip
    // this for the auth endpoints themselves to avoid redirect loops.
    if (
      res.status === 401 &&
      token &&
      typeof window !== "undefined" &&
      !path.startsWith("/auth/")
    ) {
      clearSession();
      const here = window.location.pathname + window.location.search;
      const next = here && here !== "/auth/login" ? `?next=${encodeURIComponent(here)}` : "";
      window.location.replace(`/auth/login${next}`);
    }
    throw Object.assign(new Error(err.message), { code: err.code, status: res.status });
  }
  return parsed as T;
}

function safeJson(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}

// ── Auth ─────────────────────────────────────────────────────────────────────
export const auth = {
  login: (email: string, password: string) =>
    req<LoginResponse>("/auth/login", {
      method: "POST",
      cache: "no-store",
      body: JSON.stringify({ email, password }),
    }),
  register: (email: string, password: string) =>
    req<LoginResponse>("/auth/register", {
      method: "POST",
      cache: "no-store",
      body: JSON.stringify({ email, password }),
    }),
};

// ── Series ───────────────────────────────────────────────────────────────────

export type SeriesListParams = {
  page?: number;
  limit?: number;
  genre?: string;
  status?: string;
  /** Opt into Next's ISR cache from server components. */
  revalidate?: number;
};

export type SeriesListResult = ListEnvelope<Series> & {
  page: number;
  limit: number;
};

function qs(params: Record<string, string | number | undefined>): string {
  const s = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === "" || v === null) continue;
    s.set(k, String(v));
  }
  const out = s.toString();
  return out ? `?${out}` : "";
}

export const series = {
  list: (p: SeriesListParams = {}) => {
    const { revalidate, ...qp } = p;
    const path = `/serieses/${qs(qp)}`;
    const init: ReqInit = revalidate
      ? { next: { revalidate } }
      : {};
    return req<SeriesListResult>(path, init);
  },
  trending: (limit = 10, revalidate?: number) => {
    const init: ReqInit = revalidate ? { next: { revalidate } } : {};
    return req<ListEnvelope<Series>>(`/serieses/trending${qs({ limit })}`, init);
  },
  get: (id: string, revalidate?: number) => {
    const init: ReqInit = revalidate ? { next: { revalidate } } : {};
    return req<Series>(`/serieses/${id}`, init);
  },
};

// ── Episodes ─────────────────────────────────────────────────────────────────
export const episodes = {
  list: () => req<ListEnvelope<Episode>>("/episodes/"),
  bySeries: (seriesID: string, opts: { limit?: number; revalidate?: number } = {}) => {
    const init: ReqInit = opts.revalidate ? { next: { revalidate: opts.revalidate } } : {};
    return req<ListEnvelope<Episode>>(
      `/episodes/${qs({ series_id: seriesID, limit: opts.limit })}`,
      init,
    );
  },
  get: (id: string) => req<Episode>(`/episodes/${id}`),
  pages: (episodeID: string, opts?: { revalidate?: number }) =>
    req<{ data: EpisodePage[]; total: number }>(`/episodes/${episodeID}/pages`, opts?.revalidate ? { next: { revalidate: opts.revalidate } } : {}),

  stats: (episodeID: string) =>
    req<EpisodeStats>(`/episodes/${episodeID}/stats`, { cache: "no-store" }),

  toggleLike: (episodeID: string) =>
    req<LikeResult>(`/episodes/${episodeID}/likes/toggle`, { method: "POST", cache: "no-store" }),

  comments: (episodeID: string) =>
    req<{ data: EpisodeComment[]; total: number }>(`/episodes/${episodeID}/comments`, { cache: "no-store" }),

  createComment: (
    episodeID: string,
    content: string,
    parentCommentID?: string
  ) =>
    req<EpisodeComment>(`/episodes/${episodeID}/comments`, {
      method: "POST",
      cache: "no-store",
      body: JSON.stringify(
        parentCommentID
          ? { content, parent_comment_id: parentCommentID }
          : { content }
      ),
    }),

  toggleCommentLike: (episodeID: string, commentID: string) =>
    req<CommentLikeResult>(
      `/episodes/${episodeID}/comments/${commentID}/likes/toggle`,
      { method: "POST", cache: "no-store" }
    ),
};

export type EpisodePage = {
  page_num: number;
  url: string;
  thumb_url?: string;
  width_px: number;
  height_px: number;
};

export type EpisodeStats = {
  like_count: number;
  comment_count: number;
  user_liked: boolean;
};

export type LikeResult = {
  liked: boolean;
  episode_id: string;
  like_count: number;
};

export type EpisodeComment = {
  id: string;
  user_id: string;
  content: string;
  created_at: string;
  // The comment this is a direct reply to (may itself be a reply).
  // Drives the "@user" mention chip on multi-level replies.
  parent_comment_id?: string | null;
  // The top-level ancestor of this thread. All replies of a thread share
  // the same root_comment_id and are grouped under that root in the UI.
  root_comment_id?: string | null;
  // Author of parent_comment_id, denormalized by the backend for display.
  parent_user_id?: string;
  like_count?: number;
  user_liked?: boolean;
};

export type CommentLikeResult = {
  liked: boolean;
  comment_id: string;
  like_count: number;
};

// ── Admin (requires role=admin) ──────────────────────────────────────────────
//
// Upload flow:
//   1. presignUpload() → {put_url, key}
//   2. browser PUTs bytes to put_url directly (bypasses Go API)
//   3. registerPages() with the keys → server enqueues transform jobs
//   4. poll listAdminPages() until status='ready'
export type PresignReq = {
  episode_id: string;
  filename: string;
  content_type: string;
  size: number;
};
export type PresignResp = {
  put_url: string;
  key: string;
  content_type: string;
  expires_in: number;
};
export type AdminPage = {
  id: string;
  page_num: number;
  status: "uploaded" | "processing" | "ready" | "failed";
  error?: string;
  original_url: string;
  medium_url?: string;
  thumb_url?: string;
  width_px?: number;
  height_px?: number;
};

export type AdminStats = {
  series: number;
  series_ongoing: number;
  series_completed: number;
  episodes: number;
  episodes_published: number;
  pages: number;
  pages_ready: number;
  pages_pending: number;
  pages_failed: number;
  storage_bytes: number;
  bookmarks: number;
  distinct_users: number;
  queue_depth: number;
  queue_pending: number;
  queue_active: number;
  queue_retry: number;
  queue_archived: number;
  trending: Array<{ id: string; title: string; trending_score: number }>;
  recent_uploads: AdminPage[];
  uploads_by_day: Array<{ day: string; count: number }>;
};

export type AuditEntry = {
  id: string;
  actor_id?: string;
  actor_email?: string;
  action: string;
  subject_type?: string;
  subject_id?: string;
  status: number;
  metadata: Record<string, unknown>;
  ip?: string;
  created_at: string;
};

export type UserRow = {
  user_id: string;
  bookmarks: number;
  first_seen: string;
  last_activity: string;
};

export const admin = {
  stats: () => req<AdminStats>("/admin/stats"),
  users: () => req<{ data: UserRow[]; total: number; note?: string }>("/admin/users"),
  audit: (params: { actor?: string; subject?: string; action?: string; before?: string; limit?: number } = {}) => {
    const q = qs({ ...params } as Record<string, string | number | undefined>);
    return req<{ data: AuditEntry[]; total: number; limit: number }>(`/admin/audit${q}`);
  },

  presignUpload: (r: PresignReq) =>
    req<PresignResp>("/admin/uploads/presign", {
      method: "POST",
      body: JSON.stringify(r),
    }),
  uploadTo: async (url: string, file: File) => {
    const res = await fetch(url, {
      method: "PUT",
      headers: { "Content-Type": file.type },
      body: file,
    });
    if (!res.ok) throw new Error(`upload failed: ${res.status} ${res.statusText}`);
  },
  registerPages: (
    episodeID: string,
    pages: Array<{ key: string; content_type: string; bytes: number }>,
  ) =>
    req<{ data: AdminPage[]; enqueued: number; enqueue_failed: number }>(
      `/admin/episodes/${episodeID}/pages`,
      { method: "POST", body: JSON.stringify({ pages }) },
    ),
  listPages: (episodeID: string) =>
    req<ListEnvelope<AdminPage>>(`/admin/episodes/${episodeID}/pages`),
  deletePage: (pageID: string) =>
    req<void>(`/admin/pages/${pageID}`, { method: "DELETE" }),
  reorderPages: (episodeID: string, pageIDs: string[]) =>
    req<void>(`/admin/episodes/${episodeID}/pages/reorder`, {
      method: "POST",
      body: JSON.stringify({ page_ids: pageIDs }),
    }),

  // Series mutations go through the public series handler (POST/PUT/DELETE
  // already auth-gated). Exposed here for admin UX convenience.
  createSeries: (data: Partial<Series>) =>
    req<Series>("/serieses/", { method: "POST", body: JSON.stringify(data) }),
  updateSeries: (id: string, data: Partial<Series>) =>
    req<Series>(`/serieses/${id}`, { method: "PUT", body: JSON.stringify(data) }),
  deleteSeries: (id: string) =>
    req<void>(`/serieses/${id}`, { method: "DELETE" }),

  // Episode mutations. The backend requires series_id only at create time.
  createEpisode: (data: {
    series_id: string;
    title: string;
    episode_number: number;
    thumbnail_url: string;
    published: boolean;
  }) => req<Episode>("/episodes/", { method: "POST", body: JSON.stringify(data) }),
  updateEpisode: (id: string, data: Partial<Episode>) =>
    req<Episode>(`/episodes/${id}`, { method: "PUT", body: JSON.stringify(data) }),
  deleteEpisode: (id: string) =>
    req<void>(`/episodes/${id}`, { method: "DELETE" }),
};

// ── Bookmarks (auth required) ────────────────────────────────────────────────
export const bookmarks = {
  list: () => req<ListEnvelope<Bookmark>>("/bookmarks/", { cache: "no-store" }),
  listMine: () => req<ListEnvelope<Bookmark>>("/bookmarks/", { cache: "no-store" }),
  toggle: (seriesID: string) =>
    req<{ bookmarked: boolean; series_id: string }>("/bookmarks/toggle", {
      method: "POST",
      cache: "no-store",
      body: JSON.stringify({ series_id: seriesID }),
    }),
};
