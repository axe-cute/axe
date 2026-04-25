"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useState } from "react";
import { getUser } from "@/lib/api";
import {
  LayoutDashboard,
  FolderTree,
  Users,
  ListChecks,
  Server,
  Shield,
  History,
} from "lucide-react";
import clsx from "clsx";

const NAV = [
  { href: "/admin", label: "Dashboard", icon: LayoutDashboard, exact: true },
  { href: "/admin/series", label: "Series", icon: FolderTree },
  { href: "/admin/users", label: "Users", icon: Users },
  { href: "/admin/jobs", label: "Jobs", icon: ListChecks },
  { href: "/admin/audit", label: "Audit", icon: History },
  { href: "/admin/storage", label: "Storage", icon: Server },
];

// Wrap every /admin/* page in a persistent shell. Keeps the sidebar mounted
// between navigations so client state (auth check) isn't re-run each time.
export default function AdminLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const [role, setRole] = useState<string | null>(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    setRole(getUser()?.role ?? null);
    setLoaded(true);
  }, []);

  if (!loaded) return null;
  if (role !== "admin") {
    return (
      <div className="container-gutter py-24 text-center">
        <Shield className="mx-auto text-fg-subtle" size={32} />
        <h1 className="mt-4 text-xl font-semibold">Admin only</h1>
        <p className="mt-2 text-fg-muted">
          Sign in with <code className="font-mono">admin@axe.dev</code> to access this area.
        </p>
        <Link href="/auth/login?next=/admin" className="btn-md btn-primary mt-6 inline-flex">
          Sign in
        </Link>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[220px_1fr] min-h-[calc(100vh-56px)]">
      <aside className="border-r border-border bg-bg-elevated/30">
        <div className="sticky top-14 p-4">
          <div className="label-eyebrow flex items-center gap-1 mb-4">
            <Shield size={12} /> Admin
          </div>
          <nav className="flex flex-col gap-1">
            {NAV.map((n) => {
              const active = n.exact
                ? pathname === n.href
                : pathname.startsWith(n.href);
              const Icon = n.icon;
              return (
                <Link
                  key={n.href}
                  href={n.href}
                  className={clsx(
                    "flex items-center gap-2 px-3 py-2 rounded-md text-sm transition-colors",
                    active
                      ? "bg-accent-subtle text-accent"
                      : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                  )}
                >
                  <Icon size={14} />
                  {n.label}
                </Link>
              );
            })}
          </nav>
        </div>
      </aside>

      <main className="min-w-0">{children}</main>
    </div>
  );
}
