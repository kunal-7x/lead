import type { Metadata } from 'next';
import { Search } from 'lucide-react';
import { supportLead } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Support - Capsy Admin' };

export default function SupportPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">Support Search</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Super admin gated phone lookup across tenants with full lead timeline.
        </p>
      </div>
      <section className="rounded-lg border bg-card p-4">
        <div className="mb-4 flex flex-col gap-3 md:flex-row">
          <input
            className="h-10 flex-1 rounded-md border px-3 text-sm"
            aria-label="Lead phone"
            defaultValue={supportLead.phone}
          />
          <button className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-3 text-sm text-primary-foreground">
            <Search className="h-4 w-4" aria-hidden="true" />
            Search
          </button>
        </div>
        <div className="grid gap-4 lg:grid-cols-[0.8fr_1.2fr]">
          <div className="rounded-md border p-4">
            <h2 className="text-base font-semibold">{supportLead.name}</h2>
            <dl className="mt-3 space-y-2 text-sm">
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">Tenant</dt>
                <dd>{supportLead.tenant}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">Status</dt>
                <dd>{supportLead.status}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="text-muted-foreground">Phone</dt>
                <dd>{supportLead.phone}</dd>
              </div>
            </dl>
          </div>
          <div className="rounded-md border p-4">
            <h2 className="mb-3 text-base font-semibold">Timeline</h2>
            <div className="space-y-3">
              {supportLead.timeline.map((event) => (
                <div
                  key={`${event.at}-${event.type}`}
                  className="grid grid-cols-[56px_1fr] gap-3 text-sm"
                >
                  <span className="text-muted-foreground">{event.at}</span>
                  <div>
                    <div className="font-medium">{event.type}</div>
                    <div className="text-muted-foreground">{event.detail}</div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
