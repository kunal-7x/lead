'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { cn } from '@/lib/utils';
import { canAccess, type Role } from '@/lib/rbac';
import { logout } from '@/lib/auth';
import { toast } from 'sonner';

const NAV = [
  { label: 'Dashboard', href: '/dashboard' },
  { label: 'Leads', href: '/leads' },
  { label: 'Messages', href: '/messages' },
  { label: 'Site Visits', href: '/site-visits' },
  { label: 'Billing', href: '/billing/usage' },
  { label: 'Profile', href: '/settings/profile' },
  { label: 'Team', href: '/settings/team' },
  { label: 'API Keys', href: '/settings/api-keys' },
  { label: 'Audit Log', href: '/settings/audit-log' },
];

// Roles come from the JWT stored in a cookie; for SSR we use a client-side
// store populated after hydration. For this shell stub we default to client_owner.
const USER_ROLES: Role[] = ['client_owner'];

export function ProtectedShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();

  const visibleNav = NAV.filter((item) => canAccess(item.href, USER_ROLES));

  async function handleLogout() {
    try {
      await logout();
    } catch {
      // best-effort
    }
    router.push('/login');
    toast.success('Signed out');
  }

  return (
    <div className="flex min-h-screen">
      {/* Sidebar */}
      <aside className="flex w-56 flex-col border-r bg-card px-3 py-4">
        <div className="mb-6 px-2 text-lg font-bold">EVS</div>
        <nav className="flex flex-1 flex-col gap-1">
          {visibleNav.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                'rounded-md px-3 py-2 text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground',
                pathname === item.href
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground',
              )}
            >
              {item.label}
            </Link>
          ))}
        </nav>
        <button
          onClick={handleLogout}
          className="mt-auto rounded-md px-3 py-2 text-left text-sm text-muted-foreground hover:bg-accent"
        >
          Sign out
        </button>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto p-6">{children}</main>
    </div>
  );
}
