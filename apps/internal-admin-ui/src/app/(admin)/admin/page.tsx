import type { Metadata } from 'next';
import Link from 'next/link';
import { Activity, AlertTriangle, ArrowRight, Bot, Building2, ShieldCheck } from 'lucide-react';
import { auditRows, failedJobs, providers, tenants } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Overview - Capsy Admin' };

const metrics = [
  {
    label: 'Active tenants',
    value: tenants.filter((tenant) => tenant.status === 'active').length,
    icon: Building2,
  },
  {
    label: 'Open provider circuits',
    value: providers.filter((provider) => provider.circuit === 'open').length,
    icon: Activity,
  },
  { label: 'Failed jobs', value: failedJobs.length, icon: AlertTriangle },
  { label: 'Audit events', value: auditRows.length, icon: ShieldCheck },
];

export default function AdminOverviewPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-normal">Internal Admin</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">
          Tenant operations, provider controls, support search, audit trails, feature flags, and AI
          quality review for Capsy.
        </p>
      </div>

      <section className="grid gap-3 md:grid-cols-4" aria-label="Admin health summary">
        {metrics.map((metric) => {
          const Icon = metric.icon;
          return (
            <div key={metric.label} className="rounded-lg border bg-card p-4">
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">{metric.label}</span>
                <Icon className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
              </div>
              <div className="mt-3 text-2xl font-semibold">{metric.value}</div>
            </div>
          );
        })}
      </section>

      <section className="grid gap-4 xl:grid-cols-[1.1fr_0.9fr]">
        <div className="rounded-lg border bg-card p-4">
          <div className="mb-4 flex items-center justify-between">
            <h2 className="text-base font-semibold">Priority Controls</h2>
            <Bot className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <AdminLink href="/admin/models" title="Model switcher">
              LLM, STT, TTS global and tenant overrides
            </AdminLink>
            <AdminLink href="/providers" title="Emergency provider controls">
              Circuit breaks and kill switches
            </AdminLink>
            <AdminLink href="/support" title="Cross-tenant lead search">
              Super admin gated timeline lookup
            </AdminLink>
            <AdminLink href="/quality" title="AI quality queue">
              Review stub for Phase 22
            </AdminLink>
          </div>
        </div>

        <div className="rounded-lg border bg-card p-4">
          <h2 className="mb-4 text-base font-semibold">Recent Audit</h2>
          <div className="space-y-3">
            {auditRows.map((row) => (
              <div key={`${row.actor}-${row.at}`} className="rounded-md border p-3 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <span className="font-medium">{row.action}</span>
                  <span className="text-xs text-muted-foreground">{row.at}</span>
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {row.actor} on {row.target} with {row.ticket}
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>
    </div>
  );
}

function AdminLink({
  href,
  title,
  children,
}: {
  href: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Link className="rounded-md border p-3 text-sm hover:bg-accent" href={href}>
      {title}
      <span className="mt-2 flex items-center gap-1 text-xs text-muted-foreground">
        {children} <ArrowRight className="h-3 w-3" />
      </span>
    </Link>
  );
}
