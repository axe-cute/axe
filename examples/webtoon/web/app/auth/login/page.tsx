"use client";
import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { auth, setSession } from "@/lib/api";
import { LogIn } from "lucide-react";
import { AuthShell } from "../_components/AuthShell";

export default function LoginPage() {
  return (
    <Suspense fallback={<AuthShell title="Sign in" subtitle="Loading..."><div /></AuthShell>}>
      <LoginForm />
    </Suspense>
  );
}

function LoginForm() {
  const router = useRouter();
  const sp = useSearchParams();
  const next = sp.get("next") || "/";

  const [email, setEmail] = useState("reader@axe.dev");
  const [password, setPassword] = useState("demo1234");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      const resp = await auth.login(email, password);
      setSession(resp);
      router.push(next);
    } catch (e: any) {
      setErr(e?.message ?? "Sign in failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthShell title="Sign in" subtitle="Welcome back to webtoon.">
      <form onSubmit={submit} className="space-y-4">
        <Field label="Email" htmlFor="email">
          <input
            id="email"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input"
            placeholder="you@example.com"
          />
        </Field>
        <Field label="Password" htmlFor="password">
          <input
            id="password"
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input"
            placeholder="••••••••"
          />
        </Field>

        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 text-danger text-sm px-3 py-2">
            {err}
          </div>
        )}

        <button type="submit" disabled={loading} className="btn-md btn-primary w-full gap-2">
          <LogIn size={16} />
          {loading ? "Signing in…" : "Sign in"}
        </button>

        <div className="text-xs text-fg-subtle pt-2">
          Demo mode: any email + password (≥4 chars) creates a stable session.
        </div>
      </form>

      <div className="mt-6 text-sm text-fg-muted">
        New here?{" "}
        <Link href={`/auth/register?next=${encodeURIComponent(next)}`} className="text-accent hover:underline">
          Create an account
        </Link>
      </div>
    </AuthShell>
  );
}


function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label htmlFor={htmlFor} className="label-eyebrow">
        {label}
      </label>
      <div className="mt-1">{children}</div>
    </div>
  );
}
