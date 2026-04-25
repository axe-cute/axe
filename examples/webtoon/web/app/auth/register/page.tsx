"use client";
import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { auth, setSession } from "@/lib/api";
import { UserPlus } from "lucide-react";
import { AuthShell } from "../_components/AuthShell";

export default function RegisterPage() {
  return (
    <Suspense fallback={<AuthShell title="Create account" subtitle="Loading..."><div /></AuthShell>}>
      <RegisterForm />
    </Suspense>
  );
}

function RegisterForm() {
  const router = useRouter();
  const sp = useSearchParams();
  const next = sp.get("next") || "/";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setLoading(true);
    try {
      const resp = await auth.register(email, password);
      setSession(resp);
      router.push(next);
    } catch (e: any) {
      setErr(e?.message ?? "Registration failed");
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthShell
      title="Create your account"
      subtitle="Bookmark series and pick up where you left off."
    >
      <form onSubmit={submit} className="space-y-4">
        <div>
          <label htmlFor="email" className="label-eyebrow">
            Email
          </label>
          <input
            id="email"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="input mt-1"
            placeholder="you@example.com"
          />
        </div>
        <div>
          <label htmlFor="password" className="label-eyebrow">
            Password
          </label>
          <input
            id="password"
            type="password"
            required
            minLength={4}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="input mt-1"
            placeholder="At least 4 characters"
          />
        </div>

        {err && (
          <div className="rounded border border-danger/40 bg-danger/10 text-danger text-sm px-3 py-2">
            {err}
          </div>
        )}

        <button
          type="submit"
          disabled={loading}
          className="btn-md btn-primary w-full gap-2"
        >
          <UserPlus size={16} />
          {loading ? "Creating…" : "Create account"}
        </button>
      </form>

      <div className="mt-6 text-sm text-fg-muted">
        Already have an account?{" "}
        <Link
          href={`/auth/login?next=${encodeURIComponent(next)}`}
          className="text-accent hover:underline"
        >
          Sign in
        </Link>
      </div>
    </AuthShell>
  );
}
