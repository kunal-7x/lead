import type { Metadata } from 'next';
import { PauseCircle, ShieldAlert } from 'lucide-react';
import { providers } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Providers - Capsy Admin' };

export default function ProvidersPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">Provider Health</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Live provider status, rolling failure rate, queue pressure, and manual circuit breaks.
        </p>
      </div>

      <section className="rounded-lg border bg-card p-4">
        <div className="mb-4 grid gap-3 md:grid-cols-[1fr_1fr_auto]">
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Circuit reason"
            placeholder="Reason"
          />
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Circuit ticket id"
            placeholder="Ticket id"
          />
          <button className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-3 text-sm text-primary-foreground">
            <ShieldAlert className="h-4 w-4" aria-hidden="true" />
            Emergency controls
          </button>
        </div>
        <div className="overflow-x-auto">
          <table
            className="w-full min-w-[760px] text-left text-sm"
            aria-label="Provider health table"
          >
            <thead className="border-b text-xs uppercase text-muted-foreground">
              <tr>
                <th className="py-2 pr-3 font-medium">Provider</th>
                <th className="py-2 pr-3 font-medium">Status</th>
                <th className="py-2 pr-3 font-medium">Failure rate</th>
                <th className="py-2 pr-3 font-medium">P95 latency</th>
                <th className="py-2 pr-3 font-medium">In flight</th>
                <th className="py-2 font-medium">Circuit</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((provider) => (
                <tr key={provider.key} className="border-b last:border-b-0">
                  <td className="py-3 pr-3 font-medium">{provider.name}</td>
                  <td className="py-3 pr-3">
                    <span className={provider.online ? 'text-emerald-700' : 'text-red-700'}>
                      {provider.online ? 'online' : 'offline'}
                    </span>
                  </td>
                  <td className="py-3 pr-3">{provider.failure}</td>
                  <td className="py-3 pr-3">{provider.latency}</td>
                  <td className="py-3 pr-3">{provider.inFlight}</td>
                  <td className="py-3">
                    <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs">
                      <PauseCircle className="h-3 w-3" aria-hidden="true" />
                      {provider.circuit === 'open' ? 'Open' : 'Break'}
                    </button>
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
