"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { LogIn, X, Loader2, UserPlus } from "lucide-react";
import { auth, getToken, getUser, setSession } from "@/lib/api";

type User = { id: string; email: string; role: string };

type AuthContextValue = {
  isLoggedIn: boolean;
  user: User | null;
  /**
   * Run `action` immediately if logged in. Otherwise open the inline login
   * modal and run `action` automatically after a successful login.
   */
  requireAuth: (action: () => void | Promise<void>) => void;
  openLogin: () => void;
  logout: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [open, setOpen] = useState(false);
  const pendingActionRef = useRef<(() => void | Promise<void>) | null>(null);

  // Initialize from localStorage and listen for auth changes
  useEffect(() => {
    function sync() {
      const u = getUser();
      setUser(u);
      setIsLoggedIn(!!getToken());
    }
    sync();
    window.addEventListener("webtoon:auth", sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener("webtoon:auth", sync);
      window.removeEventListener("storage", sync);
    };
  }, []);

  const requireAuth = useCallback(
    (action: () => void | Promise<void>) => {
      if (getToken()) {
        // Already authenticated — run immediately
        void action();
        return;
      }
      pendingActionRef.current = action;
      setOpen(true);
    },
    []
  );

  const openLogin = useCallback(() => {
    pendingActionRef.current = null;
    setOpen(true);
  }, []);

  const logout = useCallback(() => {
    if (typeof window === "undefined") return;
    window.localStorage.removeItem("webtoon:token");
    window.localStorage.removeItem("webtoon:user");
    window.dispatchEvent(new Event("webtoon:auth"));
  }, []);

  const handleLoggedIn = useCallback(() => {
    setIsLoggedIn(true);
    setUser(getUser());
    setOpen(false);
    const action = pendingActionRef.current;
    pendingActionRef.current = null;
    if (action) {
      // Defer to next tick so consumers see updated `isLoggedIn` state
      setTimeout(() => {
        void action();
      }, 0);
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ isLoggedIn, user, requireAuth, openLogin, logout }),
    [isLoggedIn, user, requireAuth, openLogin, logout]
  );

  return (
    <AuthContext.Provider value={value}>
      {children}
      <LoginModal
        open={open}
        onClose={() => {
          pendingActionRef.current = null;
          setOpen(false);
        }}
        onLoggedIn={handleLoggedIn}
      />
    </AuthContext.Provider>
  );
}

// ─── Modal ───────────────────────────────────────────────────────────────────

function LoginModal({
  open,
  onClose,
  onLoggedIn,
}: {
  open: boolean;
  onClose: () => void;
  onLoggedIn: () => void;
}) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("reader@axe.dev");
  const [password, setPassword] = useState("demo1234");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const emailRef = useRef<HTMLInputElement | null>(null);

  // Reset & focus on open
  useEffect(() => {
    if (!open) return;
    setErr(null);
    setLoading(false);
    const t = setTimeout(() => emailRef.current?.focus(), 50);
    return () => clearTimeout(t);
  }, [open]);

  // Esc to close
  useEffect(() => {
    if (!open) return;
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  // Lock background scroll
  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  }, [open]);

  if (!open) return null;

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (loading) return;
    setErr(null);
    setLoading(true);
    try {
      const resp =
        mode === "login"
          ? await auth.login(email, password)
          : await auth.register(email, password);
      setSession(resp);
      onLoggedIn();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Sign in failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center px-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="auth-modal-title"
    >
      <div
        className="absolute inset-0 bg-black/40 backdrop-blur-sm animate-[fadeIn_120ms_ease-out]"
        onClick={onClose}
      />
      <div className="relative w-full max-w-sm bg-white rounded-2xl shadow-2xl overflow-hidden animate-[scaleIn_160ms_ease-out]">
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="absolute top-3 right-3 p-1.5 rounded-full text-neutral-400 hover:text-neutral-700 hover:bg-neutral-100 transition-colors"
        >
          <X size={16} />
        </button>

        <div className="px-6 pt-7 pb-6">
          <div className="flex items-center gap-2 mb-1">
            <div className="w-8 h-8 rounded-full bg-[#00DC64]/10 flex items-center justify-center text-[#00DC64]">
              {mode === "login" ? <LogIn size={16} /> : <UserPlus size={16} />}
            </div>
            <h2 id="auth-modal-title" className="text-base font-bold text-neutral-900">
              {mode === "login" ? "Sign in to continue" : "Create your account"}
            </h2>
          </div>
          <p className="text-xs text-neutral-500 mb-5">
            {mode === "login"
              ? "Sign in to like, comment, and subscribe."
              : "Sign up in seconds — demo mode accepts any email."}
          </p>

          <form onSubmit={submit} className="space-y-3">
            <div>
              <label htmlFor="auth-email" className="block text-[11px] font-medium text-neutral-500 uppercase tracking-wide mb-1">
                Email
              </label>
              <input
                ref={emailRef}
                id="auth-email"
                type="email"
                required
                autoComplete="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="w-full px-3 py-2 rounded-lg bg-[#F3F4F5] text-sm text-neutral-900 placeholder:text-neutral-400 focus:outline-none focus:ring-2 focus:ring-[#00DC64]/40"
                placeholder="you@example.com"
              />
            </div>
            <div>
              <label htmlFor="auth-password" className="block text-[11px] font-medium text-neutral-500 uppercase tracking-wide mb-1">
                Password
              </label>
              <input
                id="auth-password"
                type="password"
                required
                minLength={4}
                autoComplete={mode === "login" ? "current-password" : "new-password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="w-full px-3 py-2 rounded-lg bg-[#F3F4F5] text-sm text-neutral-900 placeholder:text-neutral-400 focus:outline-none focus:ring-2 focus:ring-[#00DC64]/40"
                placeholder="••••••••"
              />
            </div>

            {err && (
              <div className="rounded-lg bg-red-50 text-red-700 text-xs px-3 py-2 border border-red-100">
                {err}
              </div>
            )}

            <button
              type="submit"
              disabled={loading}
              className="w-full flex items-center justify-center gap-1.5 px-4 py-2.5 rounded-full text-sm font-bold text-black bg-[#00DC64] hover:bg-[#00B857] transition-colors disabled:opacity-50"
            >
              {loading ? (
                <Loader2 size={14} className="animate-spin" />
              ) : mode === "login" ? (
                <LogIn size={14} />
              ) : (
                <UserPlus size={14} />
              )}
              {loading
                ? mode === "login" ? "Signing in…" : "Creating account…"
                : mode === "login" ? "Sign in" : "Create account"}
            </button>
          </form>

          <div className="mt-4 text-center text-xs text-neutral-500">
            {mode === "login" ? (
              <>
                New here?{" "}
                <button
                  type="button"
                  onClick={() => { setErr(null); setMode("register"); }}
                  className="text-[#00DC64] font-medium hover:underline"
                >
                  Create an account
                </button>
              </>
            ) : (
              <>
                Already a member?{" "}
                <button
                  type="button"
                  onClick={() => { setErr(null); setMode("login"); }}
                  className="text-[#00DC64] font-medium hover:underline"
                >
                  Sign in
                </button>
              </>
            )}
          </div>

          <p className="mt-4 text-[10px] text-neutral-400 text-center leading-relaxed">
            Demo mode: any email + password (≥4 chars) works.
          </p>
        </div>
      </div>

      <style jsx>{`
        @keyframes fadeIn {
          from { opacity: 0; }
          to { opacity: 1; }
        }
        @keyframes scaleIn {
          from { opacity: 0; transform: scale(0.96) translateY(4px); }
          to { opacity: 1; transform: scale(1) translateY(0); }
        }
      `}</style>
    </div>
  );
}
