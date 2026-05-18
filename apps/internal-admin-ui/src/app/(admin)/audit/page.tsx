import type { Metadata } from 'next';
import { auditRows } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Audit - Capsy Admin' };

export default function AuditPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">Audit Log</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Filter internal admin actions by tenant, actor, action, and time.
        </p>
      </div>
      <section className="rounded-lg border bg-card p-4">
        <div className="mb-4 grid gap-3 md:grid-cols-4">
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Tenant filter"
            placeholder="Tenant"
          />
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Actor filter"
            placeholder="Actor"
          />
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Action filter"
            placeholder="Action"
          />
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Time filter"
            placeholder="Last 24 hours"
          />
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] text-left text-sm" aria-label="Audit log table">
            <thead className="border-b text-xs uppercase text-muted-foreground">
              <tr>
                <th className="py-2 pr-3 font-medium">Actor</th>
                <th className="py-2 pr-3 font-medium">Role</th>
                <th className="py-2 pr-3 font-medium">Action</th>
                <th className="py-2 pr-3 font-medium">Target</th>
                <th className="py-2 pr-3 font-medium">Ticket</th>
                <th className="py-2 pr-3 font-medium">Reason</th>
                <th className="py-2 font-medium">Time</th>
              </tr>
            </thead>
            <tbody>
              {auditRows.map((row) => (
                <tr
                  key={`${row.actor}-${row.action}-${row.at}`}
                  className="border-b last:border-b-0"
                >
                  <td className="py-3 pr-3">{row.actor}</td>
                  <td className="py-3 pr-3">{row.role}</td>
                  <td className="py-3 pr-3 font-medium">{row.action}</td>
                  <td className="py-3 pr-3">{row.target}</td>
                  <td className="py-3 pr-3">{row.ticket}</td>
                  <td className="py-3 pr-3">{row.reason}</td>
                  <td className="py-3">{row.at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
