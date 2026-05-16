"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/utils";

const NAV = [
  { label: "Tenants", href: "/tenants" },
  { label: "Providers", href: "/providers" },
  { label: "Jobs", href: "/jobs" },
  { label: "Support", href: "/support" },
  { label: "Audit", href: "/audit" },
];

export function AdminShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-56 flex-col border-r bg-card px-3 py-4">
        <div className="mb-6 px-2">
          <span className="text-lg font-bold">EVS</span>
          <span className="ml-2 rounded bg-primary px-1 py-0.5 text-xs text-primary-foreground">
            Internal
          </span>
        </div>
        <nav className="flex flex-1 flex-col gap-1">
          {NAV.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground",
                pathname?.startsWith(item.href)
                  ? "bg-accent text-accent-foreground"
                  : "text-muted-foreground"
              )}
            >
              {item.label}
            </Link>
          ))}
        </nav>
      </aside>
      <main className="flex-1 overflow-auto p-6">{children}</main>
    </div>
  );
}
