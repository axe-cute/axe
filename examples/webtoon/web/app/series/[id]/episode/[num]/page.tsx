"use client";

import Image from "next/image";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import {
  useEffect,
  useRef,
  useState,
  useCallback,
  useMemo,
} from "react";
import {
  series as seriesApi,
  episodes as episodesApi,
  bookmarks,
  getToken,
  getUser,
  type EpisodePage,
  type EpisodeStats,
  type EpisodeComment,
} from "@/lib/api";
import { type Episode, type Series } from "@/lib/types";
import { useAuth } from "@/components/AuthProvider";
import {
  ArrowLeft,
  ChevronLeft,
  ChevronRight,
  Heart,
  MessageCircle,
  ListOrdered,
  Bookmark,
  X,
  Loader2,
  Send,
  ThumbsUp,
  MoreHorizontal,
  CornerDownRight,
  ChevronDown,
  ChevronUp,
} from "lucide-react";

const DEMO_PAGE_COUNT = 6;
const SCROLL_HIDE_THRESHOLD = 10;

function relativeTime(dateStr: string): string {
  const d = new Date(dateStr);
  const now = new Date();
  const diffSec = Math.max(0, Math.floor((now.getTime() - d.getTime()) / 1000));
  if (diffSec < 60) return "Just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffH = Math.floor(diffMin / 60);
  if (diffH < 24) return `${diffH}h ago`;
  const diffD = Math.floor(diffH / 24);
  if (diffD < 7) return `${diffD}d ago`;
  if (diffD < 30) return `${Math.floor(diffD / 7)}w ago`;
  return d.toLocaleDateString();
}

function avatarColor(userId: string): { bg: string; text: string } {
  const colors = [
    { bg: "bg-red-100", text: "text-red-600" },
    { bg: "bg-orange-100", text: "text-orange-600" },
    { bg: "bg-amber-100", text: "text-amber-600" },
    { bg: "bg-green-100", text: "text-green-600" },
    { bg: "bg-emerald-100", text: "text-emerald-600" },
    { bg: "bg-teal-100", text: "text-teal-600" },
    { bg: "bg-cyan-100", text: "text-cyan-600" },
    { bg: "bg-sky-100", text: "text-sky-600" },
    { bg: "bg-blue-100", text: "text-blue-600" },
    { bg: "bg-indigo-100", text: "text-indigo-600" },
    { bg: "bg-violet-100", text: "text-violet-600" },
    { bg: "bg-purple-100", text: "text-purple-600" },
    { bg: "bg-fuchsia-100", text: "text-fuchsia-600" },
    { bg: "bg-pink-100", text: "text-pink-600" },
    { bg: "bg-rose-100", text: "text-rose-600" },
  ];
  let hash = 0;
  for (let i = 0; i < userId.length; i++) hash = (hash * 31 + userId.charCodeAt(i)) >>> 0;
  return colors[hash % colors.length];
}

function formatUserName(userId: string, email?: string): string {
  if (email) {
    const name = email.split("@")[0];
    if (name && name.length > 1) return name.charAt(0).toUpperCase() + name.slice(1);
  }
  return "User_" + userId.slice(0, 6).toLowerCase();
}

// Format counts: 1234 → "1.2K", 1500000 → "1.5M"
function formatCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "";
  if (n < 1000) return String(n);
  if (n < 10_000) return (n / 1000).toFixed(1).replace(/\.0$/, "") + "K";
  if (n < 1_000_000) return Math.floor(n / 1000) + "K";
  if (n < 10_000_000) return (n / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M";
  return Math.floor(n / 1_000_000) + "M";
}

export default function ReaderPage() {
  const { id: seriesID, num } = useParams<{ id: string; num: string }>();
  const router = useRouter();
  const n = Number.parseInt(num, 10);

  const [series, setSeries] = useState<Series | null>(null);
  const [episodes, setEpisodes] = useState<Episode[]>([]);
  const [pages, setPages] = useState<EpisodePage[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  const [headerVisible, setHeaderVisible] = useState(true);
  const [barVisible, setBarVisible] = useState(true);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [progress, setProgress] = useState(0);
  const lastScrollY = useRef(0);
  const ticking = useRef(false);

  // Interaction state
  const [likeCount, setLikeCount] = useState(0);
  const [userLiked, setUserLiked] = useState(false);
  const [commentCount, setCommentCount] = useState(0);
  const [subscribed, setSubscribed] = useState(false);
  const [comments, setComments] = useState<EpisodeComment[]>([]);
  const [commentText, setCommentText] = useState("");
  const [commentLoading, setCommentLoading] = useState(false);

  // Comment interactions
  const [commentLikes, setCommentLikes] = useState<Record<string, { liked: boolean; count: number }>>({});
  const [replyingTo, setReplyingTo] = useState<string | null>(null);
  const [replyText, setReplyText] = useState("");
  const [replyLoading, setReplyLoading] = useState(false);
  const [expandedReplies, setExpandedReplies] = useState<Record<string, boolean>>({});

  const { isLoggedIn, requireAuth } = useAuth();

  // Fetch data + stats in one pass so correct values appear immediately
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    async function load() {
      try {
        const loggedIn = !!getToken();

        const [s, list] = await Promise.all([
          seriesApi.get(seriesID).catch(() => null),
          episodesApi.bySeries(seriesID, { limit: 200 }).catch(() => ({ data: [] as Episode[], total: 0 })),
        ]);
        if (cancelled) return;
        if (!s || Number.isNaN(n)) {
          setErr("Not found");
          return;
        }
        const sorted = [...list.data].sort(
          (a, b) => a.episode_number - b.episode_number
        );

        const current = sorted.find((e) => e.episode_number === n);
        if (!current) {
          setErr("Episode not found");
          return;
        }

        const [pageEnv, stats, cmts] = await Promise.all([
          episodesApi.pages(current.id).catch(() => ({ data: [] as EpisodePage[], total: 0 })),
          episodesApi.stats(current.id).catch(() => ({ like_count: 0, comment_count: 0, user_liked: false })),
          episodesApi.comments(current.id).catch(() => ({ data: [] as EpisodeComment[], total: 0 })),
        ]);
        if (cancelled) return;

        if (loggedIn) {
          try {
            const bmList = await bookmarks.list().catch(() => ({ data: [] as { series_id: string }[], total: 0 }));
            const found = bmList.data.some((b) => b.series_id === s.id);
            if (!cancelled) setSubscribed(found);
          } catch { /* ignore */ }
        }

        if (!cancelled) {
          setSeries(s);
          setEpisodes(sorted);
          setPages(pageEnv.data);
          setLikeCount(stats.like_count);
          setUserLiked(stats.user_liked);
          setCommentCount(cmts.total);
          setComments(cmts.data);
          // Hydrate per-comment like state from API response
          const likes: Record<string, { liked: boolean; count: number }> = {};
          for (const c of cmts.data) {
            likes[c.id] = {
              liked: !!c.user_liked,
              count: c.like_count ?? 0,
            };
          }
          setCommentLikes(likes);
        }
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    load();
    return () => { cancelled = true; };
  }, [seriesID, n, isLoggedIn]);

  const current = useMemo(
    () => episodes.find((e) => e.episode_number === n),
    [episodes, n]
  );

  // Group comments into a 1-level visual tree: top-level + replies-by-root.
  // Backend orders top-level newest-first and replies chrono-asc within each
  // root. Multi-level replies (reply-to-reply) collapse into the same thread
  // by `root_comment_id` while preserving `parent_comment_id` for @mentions.
  const { topLevelComments, repliesByRoot } = useMemo(() => {
    const top: EpisodeComment[] = [];
    const replies: Record<string, EpisodeComment[]> = {};
    for (const c of comments) {
      const root = c.root_comment_id ?? c.parent_comment_id ?? null;
      if (root) {
        (replies[root] ||= []).push(c);
      } else {
        top.push(c);
      }
    }
    return { topLevelComments: top, repliesByRoot: replies };
  }, [comments]);
  const idx = current ? episodes.indexOf(current) : -1;
  const prev = idx > 0 ? episodes[idx - 1] : null;
  const next = idx < episodes.length - 1 ? episodes[idx + 1] : null;

  // Scroll tracking: header/bar auto-hide + progress bar
  useEffect(() => {
    function onScroll() {
      if (ticking.current) return;
      ticking.current = true;
      requestAnimationFrame(() => {
        const y = window.scrollY || document.documentElement.scrollTop;
        const max = document.documentElement.scrollHeight - window.innerHeight;
        setProgress(max > 0 ? (y / max) * 100 : 0);

        const delta = y - lastScrollY.current;
        if (delta > SCROLL_HIDE_THRESHOLD && y > 80) {
          setHeaderVisible(false);
          setBarVisible(false);
        } else if (delta < -SCROLL_HIDE_THRESHOLD) {
          setHeaderVisible(true);
          setBarVisible(true);
        }
        lastScrollY.current = y;
        ticking.current = false;
      });
    }
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  // Keyboard navigation
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (drawerOpen) {
        if (e.key === "Escape") setDrawerOpen(false);
        return;
      }
      if (e.key === "ArrowLeft" && prev) {
        router.push(`/series/${seriesID}/episode/${prev.episode_number}`);
      } else if (e.key === "ArrowRight" && next) {
        router.push(`/series/${seriesID}/episode/${next.episode_number}`);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [drawerOpen, prev, next, router, seriesID]);

  const onToggleLike = useCallback(async () => {
    requireAuth(async () => {
      if (!current) return;
      // Optimistic update
      setUserLiked((v) => !v);
      setLikeCount((c) => (userLiked ? c - 1 : c + 1));
      try {
        const res = await episodesApi.toggleLike(current.id);
        setUserLiked(res.liked);
        setLikeCount(res.like_count);
      } catch {
        // Revert on error
        setUserLiked(userLiked);
        setLikeCount(likeCount);
      }
    });
  }, [current, userLiked, likeCount, requireAuth]);

  const onToggleSubscribe = useCallback(async () => {
    requireAuth(async () => {
      if (!series) return;
      try {
        const res = await bookmarks.toggle(series.id);
        setSubscribed(res.bookmarked);
      } catch {
        // ignore
      }
    });
  }, [series, requireAuth]);

  const onSubmitComment = useCallback(async () => {
    requireAuth(async () => {
      if (!current || !commentText.trim()) return;
      setCommentLoading(true);
      try {
        const cmt = await episodesApi.createComment(current.id, commentText.trim());
        setComments((prev) => [cmt, ...prev]);
        setCommentCount((n) => n + 1);
        setCommentText("");
      } catch (e) {
        alert(e instanceof Error ? e.message : "Failed to post comment");
      } finally {
        setCommentLoading(false);
      }
    });
  }, [current, commentText, requireAuth]);

  const onToggleCommentLike = useCallback((commentId: string) => {
    requireAuth(async () => {
      if (!current) return;
      // Optimistic update
      let prevState: { liked: boolean; count: number } | undefined;
      setCommentLikes((prev) => {
        const curr = prev[commentId] || { liked: false, count: 0 };
        prevState = curr;
        const nextLiked = !curr.liked;
        return { ...prev, [commentId]: { liked: nextLiked, count: Math.max(0, curr.count + (nextLiked ? 1 : -1)) } };
      });
      try {
        const res = await episodesApi.toggleCommentLike(current.id, commentId);
        setCommentLikes((prev) => ({
          ...prev,
          [commentId]: { liked: res.liked, count: res.like_count },
        }));
      } catch (e) {
        // Revert on error
        if (prevState) {
          setCommentLikes((prev) => ({ ...prev, [commentId]: prevState! }));
        }
        alert(e instanceof Error ? e.message : "Failed to toggle like");
      }
    });
  }, [current, requireAuth]);

  const onSubmitReply = useCallback(async (parentCommentId: string) => {
    requireAuth(async () => {
      if (!current || !replyText.trim()) return;
      setReplyLoading(true);
      try {
        const cmt = await episodesApi.createComment(current.id, replyText.trim(), parentCommentId);
        // Replies append at end of the parent's reply group (chronological)
        setComments((prev) => [...prev, cmt]);
        setReplyText("");
        setReplyingTo(null);
      } catch (e) {
        alert(e instanceof Error ? e.message : "Failed to post reply");
      } finally {
        setReplyLoading(false);
      }
    });
  }, [current, replyText, requireAuth]);

  const goto = useCallback(
    (ep: Episode) => {
      setDrawerOpen(false);
      router.push(`/series/${seriesID}/episode/${ep.episode_number}`);
    },
    [router, seriesID]
  );

  if (loading) {
    return (
      <div className="min-h-screen bg-white flex items-center justify-center">
        <Loader2 className="animate-spin text-[#00DC64]" size={32} />
      </div>
    );
  }

  if (err || !series || !current) {
    return (
      <div className="min-h-screen bg-white flex flex-col items-center justify-center gap-3 text-neutral-900">
        <p className="text-sm text-neutral-500">{err ?? "Not found"}</p>
        <Link href={`/series/${seriesID}`} className="text-sm font-medium text-[#03AA5A]">
          Back to series
        </Link>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-white text-neutral-900">
      {/* Progress bar */}
      <div className="reader-progress" style={{ transform: `scaleX(${progress / 100})`, transformOrigin: "left" }} />

      {/* Tap zones (desktop only) */}
      {prev && (
        <div
          className="reader-tap-zone left-0"
          onClick={() => router.push(`/series/${seriesID}/episode/${prev.episode_number}`)}
          title={`Previous: Ep ${prev.episode_number}`}
        />
      )}
      {next && (
        <div
          className="reader-tap-zone right-0"
          onClick={() => router.push(`/series/${seriesID}/episode/${next.episode_number}`)}
          title={`Next: Ep ${next.episode_number}`}
        />
      )}

      {/* Header */}
      <header className={`reader-header ${headerVisible ? "" : "reader-header-hidden"}`}>
        <div className="mx-auto h-full max-w-[1200px] px-4 flex items-center gap-3">
          <Link
            href={`/series/${series.id}`}
            className="flex items-center gap-2 text-neutral-900 hover:text-[#03AA5A] transition-colors text-sm"
          >
            <ArrowLeft size={16} />
            <span className="hidden sm:inline font-medium">Back</span>
          </Link>
          <div className="flex-1 min-w-0 truncate text-sm">
            <span className="font-medium">{series.title}</span>
            <span className="mx-2 text-neutral-300">|</span>
            <span className="text-neutral-500">
              Ep {current.episode_number} · {current.title}
            </span>
          </div>
          <div className="hidden sm:flex items-center gap-1 text-xs text-neutral-400 font-mono">
            {idx + 1} / {episodes.length}
          </div>
        </div>
      </header>

      {/* Top padding for fixed header */}
      <div className="h-[52px]" />

      {/* Social bar (under header) */}
      <div className="mx-auto max-w-[800px] px-4 py-3 flex items-center justify-between border-b border-[#e0e0e0]">
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={onToggleLike}
            aria-label={`${userLiked ? "Unlike" : "Like"} episode${likeCount ? ` (${likeCount} likes)` : ""}`}
            className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-full text-sm font-medium transition-colors ${
              userLiked
                ? "text-[#E74C3C] bg-[#E74C3C]/10 hover:bg-[#E74C3C]/15"
                : "text-neutral-600 hover:text-[#E74C3C] hover:bg-neutral-100"
            }`}
          >
            <Heart size={16} fill={userLiked ? "currentColor" : "none"} strokeWidth={userLiked ? 0 : 2} />
            {likeCount > 0 && (
              <span className="tabular-nums">{formatCount(likeCount)}</span>
            )}
          </button>
          <button
            type="button"
            aria-label={`Comments${commentCount ? ` (${commentCount})` : ""}`}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-full text-sm font-medium text-neutral-600 hover:text-[#03AA5A] hover:bg-neutral-100 transition-colors"
            onClick={() => document.getElementById("comments-section")?.scrollIntoView({ behavior: "smooth" })}
          >
            <MessageCircle size={16} />
            {commentCount > 0 && (
              <span className="tabular-nums">{formatCount(commentCount)}</span>
            )}
          </button>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onToggleSubscribe}
            className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full text-sm transition-colors ${
              subscribed
                ? "bg-[#00DC64] text-black hover:bg-[#00B857]"
                : "bg-[#F3F4F5] text-neutral-900 hover:bg-[#E8E9EB]"
            }`}
          >
            <Bookmark size={14} fill={subscribed ? "currentColor" : "none"} /> {subscribed ? "Subscribed" : "Subscribe"}
          </button>
        </div>
      </div>

      {/* Pages */}
      <div className="mx-auto max-w-[800px]">
        <div className="space-y-0">
          {pages.length > 0 ? (
            pages.map((p, i) => (
              <Image
                key={p.page_num}
                src={p.url}
                alt={`Page ${p.page_num}`}
                width={p.width_px || 1200}
                height={p.height_px || 1800}
                sizes="(max-width: 768px) 100vw, 800px"
                priority={i < 2}
                loading={i < 2 ? "eager" : "lazy"}
                className="w-full h-auto block img-fade-in"
              />
            ))
          ) : (
            Array.from({ length: DEMO_PAGE_COUNT }).map((_, i) => (
              <img
                key={i}
                src={`${current.thumbnail_url.replace(/\/\d+\/\d+/, "")}/800/1200?page=${i}`}
                alt={`Page ${i + 1} (placeholder)`}
                width={800}
                height={1200}
                className="w-full h-auto block img-fade-in"
                style={{ aspectRatio: "800/1200" }}
                loading={i < 2 ? "eager" : "lazy"}
              />
            ))
          )}
        </div>

        {pages.length === 0 && (
          <p className="py-6 text-xs text-neutral-400 text-center">
            No uploaded pages yet · showing placeholders.
          </p>
        )}
      </div>

      {/* Comments section */}
      <div id="comments-section" className="mx-auto max-w-[800px] px-4 py-8 scroll-mt-[60px]">
        <div className="flex items-center justify-between mb-6">
          <h3 className="text-base font-bold text-neutral-900">
            Comments
            {commentCount > 0 && (
              <span className="text-neutral-400 font-normal ml-2 tabular-nums">{formatCount(commentCount)}</span>
            )}
          </h3>
        </div>

        {/* Comment input */}
        <div className="flex gap-3 mb-8">
          {(() => {
            const u = getUser();
            const color = avatarColor(u?.id ?? "guest");
            return (
              <div className={`w-9 h-9 rounded-full ${color.bg} flex items-center justify-center ${color.text} text-sm font-bold shrink-0 select-none`}>
                {(u?.email?.[0] ?? "?").toUpperCase()}
              </div>
            );
          })()}
          <div className="flex-1 min-w-0">
            <div
              onClick={() => {
                if (!isLoggedIn) requireAuth(() => { /* focus handled by re-render */ });
              }}
              className={!isLoggedIn ? "cursor-pointer" : ""}
            >
              <textarea
                value={commentText}
                onChange={(e) => isLoggedIn && setCommentText(e.target.value)}
                onKeyDown={(e) => {
                  if (!isLoggedIn) return;
                  if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); onSubmitComment(); }
                }}
                placeholder={isLoggedIn ? "Add a comment..." : "Log in to add a comment..."}
                rows={2}
                disabled={!isLoggedIn}
                readOnly={!isLoggedIn}
                className={`w-full px-3.5 py-2.5 rounded-xl text-sm text-neutral-900 placeholder:text-neutral-400 focus:outline-none focus:ring-2 focus:ring-[#00DC64]/40 resize-none ${isLoggedIn ? "bg-[#F3F4F5]" : "bg-neutral-100 cursor-pointer opacity-70"}`}
              />
            </div>
            {isLoggedIn && (
              <div className="flex items-center justify-end mt-2 gap-2">
                <button
                  type="button"
                  onClick={() => setCommentText("")}
                  disabled={commentLoading || !commentText}
                  className="px-3 py-1.5 rounded-full text-xs font-medium text-neutral-500 hover:bg-[#F3F4F5] transition-colors disabled:opacity-40"
                >
                  Cancel
                </button>
                <button
                  type="button"
                  onClick={onSubmitComment}
                  disabled={commentLoading || !commentText.trim()}
                  className="flex items-center gap-1.5 px-4 py-1.5 rounded-full text-xs font-bold text-black bg-[#00DC64] hover:bg-[#00B857] transition-colors disabled:opacity-50 disabled:hover:bg-[#00DC64]"
                >
                  {commentLoading ? (
                    <Loader2 size={12} className="animate-spin" />
                  ) : (
                    <Send size={12} />
                  )}
                  Comment
                </button>
              </div>
            )}
          </div>
        </div>

        {/* Comment list (tree) */}
        <div className="space-y-6">
          {topLevelComments.length === 0 ? (
            <div className="text-center py-10">
              <MessageCircle size={32} className="mx-auto text-neutral-200 mb-3" />
              <p className="text-sm text-neutral-400">No comments yet. Be the first to share your thoughts!</p>
            </div>
          ) : (
            topLevelComments.map((c) => {
              const replies = repliesByRoot[c.id] ?? [];
              const expanded = expandedReplies[c.id] ?? replies.length <= 2;
              return (
                <div key={c.id}>
                  <CommentNode
                    comment={c}
                    isReply={false}
                    commentLikes={commentLikes}
                    onToggleLike={onToggleCommentLike}
                    onReplyClick={() => setReplyingTo(replyingTo === c.id ? null : c.id)}
                    isReplying={replyingTo === c.id}
                  />
                  {/* Inline reply form (under top-level) */}
                  {replyingTo === c.id && (
                    <ReplyForm
                      placeholder={`Reply to ${getUser()?.id === c.user_id ? "yourself" : formatUserName(c.user_id)}...`}
                      replyText={replyText}
                      setReplyText={setReplyText}
                      onCancel={() => { setReplyingTo(null); setReplyText(""); }}
                      onSubmit={() => onSubmitReply(c.id)}
                      loading={replyLoading}
                      indent={48}
                    />
                  )}
                  {/* Replies thread */}
                  {replies.length > 0 && (
                    <div className="mt-3 ml-12 pl-4 border-l-2 border-neutral-100 space-y-4">
                      {!expanded ? (
                        <button
                          type="button"
                          onClick={() => setExpandedReplies((p) => ({ ...p, [c.id]: true }))}
                          className="flex items-center gap-1.5 text-xs font-semibold text-[#03AA5A] hover:text-[#028a48] transition-colors"
                        >
                          <ChevronDown size={14} />
                          View {replies.length} {replies.length === 1 ? "reply" : "replies"}
                        </button>
                      ) : (
                        <>
                          {replies.map((rc: EpisodeComment) => {
                            // Show "@user" chip when this reply targets someone
                            // *other* than the thread's top-level author (i.e.
                            // reply-to-reply mid-thread). When replying directly
                            // to the root, the visual nesting is enough.
                            const targetUserID = rc.parent_user_id;
                            const showMention =
                              !!targetUserID && targetUserID !== c.user_id;
                            const replyToName = showMention
                              ? formatUserName(targetUserID!)
                              : null;
                            return (
                              <div key={rc.id}>
                                <CommentNode
                                  comment={rc}
                                  isReply
                                  commentLikes={commentLikes}
                                  onToggleLike={onToggleCommentLike}
                                  onReplyClick={() => setReplyingTo(replyingTo === rc.id ? null : rc.id)}
                                  isReplying={replyingTo === rc.id}
                                  replyToName={replyToName}
                                />
                                {replyingTo === rc.id && (
                                  <ReplyForm
                                    placeholder={`Reply to ${formatUserName(rc.user_id)}...`}
                                    replyText={replyText}
                                    setReplyText={setReplyText}
                                    onCancel={() => { setReplyingTo(null); setReplyText(""); }}
                                    // Pass rc.id (the *direct* parent) — backend
                                    // resolves root_comment_id so the reply still
                                    // joins this same thread, and parent_user_id
                                    // drives the @mention on render.
                                    onSubmit={() => onSubmitReply(rc.id)}
                                    loading={replyLoading}
                                    indent={36}
                                  />
                                )}
                              </div>
                            );
                          })}
                          {replies.length > 2 && (
                            <button
                              type="button"
                              onClick={() => setExpandedReplies((p) => ({ ...p, [c.id]: false }))}
                              className="flex items-center gap-1.5 text-xs font-semibold text-neutral-500 hover:text-neutral-700 transition-colors"
                            >
                              <ChevronUp size={14} />
                              Hide replies
                            </button>
                          )}
                        </>
                      )}
                    </div>
                  )}
                </div>
              );
            })
          )}
        </div>
      </div>

      {/* Spacer for bottom bar */}
      <div className="h-24" />

      {/* Floating bottom bar */}
      <nav
        className={`reader-bottom-bar px-4 py-3 ${barVisible ? "" : "reader-bottom-bar-hidden"}`}
      >
        <div className="mx-auto max-w-[800px] flex items-center justify-between gap-3">
          {prev ? (
            <button
              type="button"
              onClick={() => router.push(`/series/${seriesID}/episode/${prev.episode_number}`)}
              className="flex items-center gap-1 px-3 py-2 rounded-full text-sm font-bold text-neutral-900 bg-[#F3F4F5] hover:bg-[#E8E9EB] transition-colors"
            >
              <ChevronLeft size={16} /> Ep {prev.episode_number}
            </button>
          ) : (
            <div />
          )}

          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            className="flex items-center gap-1.5 px-3 py-2 rounded-full text-sm font-bold text-neutral-900 bg-[#F3F4F5] hover:bg-[#E8E9EB] transition-colors"
          >
            <ListOrdered size={14} /> Episodes
          </button>

          {next ? (
            <button
              type="button"
              onClick={() => router.push(`/series/${seriesID}/episode/${next.episode_number}`)}
              className="flex items-center gap-1 px-4 py-2 rounded-full text-sm font-bold text-black bg-[#00DC64] hover:bg-[#00B857] transition-colors"
            >
              Ep {next.episode_number} <ChevronRight size={16} />
            </button>
          ) : (
            <span className="text-sm text-neutral-400">Latest episode</span>
          )}
        </div>
      </nav>

      {/* Episode drawer overlay */}
      {drawerOpen && (
        <div className="fixed inset-0 z-[70]" onClick={() => setDrawerOpen(false)}>
          <div className="absolute inset-0 bg-black/30" />
          <div
            className="absolute bottom-0 inset-x-0 md:right-0 md:left-auto md:top-0 md:w-[320px] md:bottom-0 drawer-enter bg-white"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between px-4 py-3 border-b border-[#e0e0e0]">
              <h3 className="font-medium text-sm">Episodes</h3>
              <button type="button" onClick={() => setDrawerOpen(false)} className="p-1 rounded hover:bg-[#F3F4F5]">
                <X size={16} />
              </button>
            </div>
            <div className="overflow-y-auto max-h-[60vh] md:max-h-[calc(100vh-52px)] no-scrollbar">
              {episodes.map((ep) => {
                const active = ep.id === current.id;
                return (
                  <button
                    type="button"
                    key={ep.id}
                    onClick={() => goto(ep)}
                    className={`w-full text-left px-4 py-3 flex items-center gap-3 border-b border-[#f5f5f5] transition-colors ${
                      active ? "bg-[#F3F4F5] font-semibold" : "hover:bg-[#fafafa]"
                    }`}
                  >
                    <span className={`text-xs font-mono w-6 ${active ? "text-[#00DC64]" : "text-neutral-400"}`}>
                      {String(ep.episode_number).padStart(2, "0")}
                    </span>
                    <span className={`text-sm truncate ${active ? "text-neutral-900" : "text-neutral-600"}`}>
                      {ep.title}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Comment tree pieces ────────────────────────────────────────────────────

function CommentNode({
  comment,
  isReply,
  commentLikes,
  onToggleLike,
  onReplyClick,
  isReplying,
  replyToName,
}: {
  comment: EpisodeComment;
  isReply: boolean;
  commentLikes: Record<string, { liked: boolean; count: number }>;
  onToggleLike: (commentID: string) => void;
  onReplyClick: () => void;
  isReplying: boolean;
  replyToName?: string | null;
}) {
  const me = getUser();
  const isMe = me?.id === comment.user_id;
  const displayName = isMe ? "You" : formatUserName(comment.user_id);
  const initial = (displayName[0] ?? "?").toUpperCase();
  const color = avatarColor(comment.user_id);
  const likeInfo = commentLikes[comment.id];
  const likeN = likeInfo?.count ?? 0;
  const avatarSize = isReply ? "w-7 h-7 text-xs" : "w-9 h-9 text-sm";
  const gap = isReply ? "gap-2.5" : "gap-3";
  return (
    <div className={`flex ${gap} group`}>
      <div className={`${avatarSize} rounded-full ${color.bg} flex items-center justify-center ${color.text} font-bold shrink-0 select-none`}>
        {initial}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1 flex-wrap">
          <span className={`font-semibold text-neutral-900 ${isReply ? "text-[13px]" : "text-sm"}`}>
            {displayName}
          </span>
          {replyToName && (
            <span className="inline-flex items-center gap-0.5 text-[11px] text-neutral-500">
              <CornerDownRight size={11} className="text-neutral-400" />
              <span className="text-[#03AA5A] font-medium">@{replyToName}</span>
            </span>
          )}
          <span className="text-xs text-neutral-300">·</span>
          <span className="text-xs text-neutral-400">
            {relativeTime(comment.created_at)}
          </span>
        </div>
        <p className={`text-neutral-700 leading-relaxed whitespace-pre-wrap break-words ${isReply ? "text-[13px]" : "text-sm"}`}>
          {comment.content}
        </p>
        <div className="flex items-center gap-4 mt-1.5">
          <button
            type="button"
            onClick={() => onToggleLike(comment.id)}
            aria-label={`${likeInfo?.liked ? "Unlike" : "Like"} comment${likeN ? ` (${likeN})` : ""}`}
            className={`flex items-center gap-1 text-xs transition-colors ${
              likeInfo?.liked ? "text-[#00DC64] font-medium" : "text-neutral-400 hover:text-neutral-600"
            }`}
          >
            <ThumbsUp size={13} fill={likeInfo?.liked ? "currentColor" : "none"} strokeWidth={likeInfo?.liked ? 0 : 2} />
            <span className="tabular-nums">{likeN > 0 ? formatCount(likeN) : "Like"}</span>
          </button>
          <button
            type="button"
            onClick={onReplyClick}
            className={`flex items-center gap-1 text-xs transition-colors ${
              isReplying ? "text-[#00DC64] font-medium" : "text-neutral-400 hover:text-neutral-600"
            }`}
          >
            <CornerDownRight size={13} /> Reply
          </button>
          <button
            type="button"
            className="text-neutral-300 hover:text-neutral-500 transition-colors opacity-0 group-hover:opacity-100"
          >
            <MoreHorizontal size={14} />
          </button>
        </div>
      </div>
    </div>
  );
}

function ReplyForm({
  placeholder,
  replyText,
  setReplyText,
  onCancel,
  onSubmit,
  loading,
  indent,
}: {
  placeholder: string;
  replyText: string;
  setReplyText: (v: string) => void;
  onCancel: () => void;
  onSubmit: () => void;
  loading: boolean;
  indent: number;
}) {
  const u = getUser();
  const color = avatarColor(u?.id ?? "me");
  return (
    <div className="mt-3 flex gap-2.5" style={{ paddingLeft: indent }}>
      <div className={`w-7 h-7 rounded-full ${color.bg} flex items-center justify-center ${color.text} text-xs font-bold shrink-0 select-none`}>
        {(u?.email?.[0] ?? "Y").toUpperCase()}
      </div>
      <div className="flex-1 min-w-0">
        <textarea
          value={replyText}
          onChange={(e) => setReplyText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              onSubmit();
            }
          }}
          placeholder={placeholder}
          rows={1}
          autoFocus
          className="w-full px-3 py-2 rounded-lg bg-[#F3F4F5] text-sm text-neutral-900 placeholder:text-neutral-400 focus:outline-none focus:ring-2 focus:ring-[#00DC64]/40 resize-none"
        />
        <div className="flex items-center justify-end mt-1.5 gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-2.5 py-1 rounded-full text-[11px] font-medium text-neutral-500 hover:bg-[#F3F4F5] transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onSubmit}
            disabled={loading || !replyText.trim()}
            className="flex items-center gap-1 px-3 py-1 rounded-full text-[11px] font-bold text-black bg-[#00DC64] hover:bg-[#00B857] transition-colors disabled:opacity-50"
          >
            {loading ? <Loader2 size={10} className="animate-spin" /> : <Send size={10} />}
            Reply
          </button>
        </div>
      </div>
    </div>
  );
}
