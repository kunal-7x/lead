'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import {
  Activity,
  Bot,
  Building2,
  ClipboardList,
  Flag,
  Gauge,
  Search,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react';
import { canSeeModelSwitcher } from '@/lib/admin-data';
import { cn } from '@/lib/utils';

const NAV = [
  { label: 'Overview', href: '/admin', icon: Gauge },
  { label: 'Models', href: '/admin/models', icon: SlidersHorizontal, superAdminOnly: true },
  { label: 'Tenants', href: '/tenants', icon: Building2 },
  { label: 'Providers', href: '/providers', icon: Activity },
  { label: 'Jobs', href: '/jobs', icon: ClipboardList },
  { label: 'Support', href: '/support', icon: Search },
  { label: 'Audit', href: '/audit', icon: ShieldCheck },
  { label: 'Flags', href: '/flags', icon: Flag },
  { label: 'AI Quality', href: '/quality', icon: Bot },
];

export function AdminShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-screen">
      <aside className="flex w-56 flex-col border-r bg-card px-3 py-4">
        <div className="mb-6 px-2">
          <span className="text-lg font-bold">Capsy</span>
          <span className="ml-2 rounded bg-primary px-1 py-0.5 text-xs text-primary-foreground">
            Internal
          </span>
        </div>
        <nav className="flex flex-1 flex-col gap-1">
          {NAV.filter((item) => !item.superAdminOnly || canSeeModelSwitcher).map((item) => {
            const Icon = item.icon;
            const active = pathname === item.href || pathname?.startsWith(`${item.href}/`);
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  'flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground',
                  active ? 'bg-accent text-accent-foreground' : 'text-muted-foreground',
                )}
              >
                <Icon className="h-4 w-4" aria-hidden="true" />
                {item.label}
              </Link>
            );
          })}
        </nav>
      </aside>
      <main className="flex-1 overflow-auto p-6">{children}</main>
    </div>
  );
}
