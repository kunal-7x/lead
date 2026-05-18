import type { Metadata } from 'next';
import { PauseCircle, PlayCircle, Trash2, UserRoundCheck } from 'lucide-react';
import { tenants } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Tenants - Capsy Admin' };

export default function TenantsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">Tenants</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Suspend, resume, impersonate, and soft-delete tenants with audited ticket metadata.
        </p>
      </div>
      <section className="rounded-lg border bg-card p-4">
        <div className="mb-4 grid gap-3 md:grid-cols-[1fr_1fr_1fr]">
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Reason"
            placeholder="Reason required for destructive ops"
          />
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Ticket id"
            placeholder="Ticket id"
          />
          <select
            className="h-10 rounded-md border bg-background px-3 text-sm"
            aria-label="Tenant status filter"
            defaultValue="all"
          >
            <option value="all">All statuses</option>
            <option value="active">Active</option>
            <option value="suspended">Suspended</option>
          </select>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] text-left text-sm" aria-label="Tenant operations">
            <thead className="border-b text-xs uppercase text-muted-foreground">
              <tr>
                <th className="py-2 pr-3 font-medium">Tenant</th>
                <th className="py-2 pr-3 font-medium">Owner</th>
                <th className="py-2 pr-3 font-medium">Plan</th>
                <th className="py-2 pr-3 font-medium">Usage</th>
                <th className="py-2 pr-3 font-medium">Status</th>
                <th className="py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tenants.map((tenant) => (
                <tr key={tenant.id} className="border-b last:border-b-0">
                  <td className="py-3 pr-3">
                    <div className="font-medium">{tenant.name}</div>
                    <div className="text-xs text-muted-foreground">{tenant.id}</div>
                  </td>
                  <td className="py-3 pr-3">{tenant.owner}</td>
                  <td className="py-3 pr-3">{tenant.plan}</td>
                  <td className="py-3 pr-3">{tenant.usage}</td>
                  <td className="py-3 pr-3">
                    <span className="rounded-md border px-2 py-1 text-xs">{tenant.status}</span>
                  </td>
                  <td className="py-3">
                    <div className="flex flex-wrap gap-2">
                      <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs">
                        <PauseCircle className="h-3 w-3" aria-hidden="true" />
                        Suspend
                      </button>
                      <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs">
                        <PlayCircle className="h-3 w-3" aria-hidden="true" />
                        Resume
                      </button>
                      <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs">
                        <UserRoundCheck className="h-3 w-3" aria-hidden="true" />
                        Impersonate
                      </button>
                      <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs text-red-700">
                        <Trash2 className="h-3 w-3" aria-hidden="true" />
                        Soft delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
