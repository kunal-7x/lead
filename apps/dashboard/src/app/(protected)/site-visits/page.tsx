'use client';

import { useQuery } from '@tanstack/react-query';
import { AlertTriangle, CalendarDays, Loader2, MoveRight } from 'lucide-react';
import { api } from '@/lib/api';

const DEMO_TENANT_ID = 'tenant-demo';

type ReportRow = { dimension: string; count: number; value: number };
type ReportResponse = {
  tenant_id: string;
  report: string;
  freshness_s: number;
  rows: ReportRow[];
};

async function fetchSiteVisitsReport(): Promise<ReportResponse> {
  const { data } = await api.get(
    `/v1/reports/site-visit?tenant_id=${DEMO_TENANT_ID}`,
  );
  return data;
}

function stateClass(count: number) {
  if (count === 0) return 'rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground';
  if (count < 3) return 'rounded-full bg-amber-50 px-2 py-0.5 text-[11px] text-amber-700';
  return 'rounded-full bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700';
}

export default function SiteVisitsPage() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['report', 'site-visit'],
    queryFn: fetchSiteVisitsReport,
  });

  const rows = data?.rows ?? [];
  const freshness = data?.freshness_s;
  const totalVisits = rows.reduce((s, r) => s + r.count, 0);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Site Visits</h1>
          <p className="text-sm text-muted-foreground">
            Visits requested by the AI agent, grouped by project.
          </p>
        </div>
        <div className="flex items-center gap-3">
          {freshness != null && (
            <span className="text-xs text-muted-foreground">
              Data freshness: {freshness}s ago
            </span>
          )}
          <button className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90">
            <CalendarDays className="h-4 w-4" />
            New visit
          </button>
        </div>
      </div>

      {/* Summary strip */}
      {!isLoading && !isError && (
        <div className="grid grid-cols-3 gap-4 sm:grid-cols-3">
          <div className="rounded-lg border bg-card p-4 text-center">
            <p className="text-xs text-muted-foreground">Total Visits</p>
            <p className="mt-1 text-2xl font-semibold">{totalVisits}</p>
          </div>
          <div className="rounded-lg border bg-card p-4 text-center">
            <p className="text-xs text-muted-foreground">Projects</p>
            <p className="mt-1 text-2xl font-semibold">{rows.length}</p>
          </div>
          <div className="rounded-lg border bg-card p-4 text-center">
            <p className="text-xs text-muted-foreground">Avg / project</p>
            <p className="mt-1 text-2xl font-semibold">
              {rows.length > 0 ? (totalVisits / rows.length).toFixed(1) : '—'}
            </p>
          </div>
        </div>
      )}

      <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
        {/* Main table */}
        <section className="overflow-hidden rounded-lg border bg-card">
          <div className="border-b px-4 py-3">
            <h2 className="text-sm font-semibold">Visits by Project</h2>
          </div>

          {isLoading && (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Loading site visit data…
            </div>
          )}

          {isError && (
            <div className="flex items-center justify-center py-12 text-sm text-destructive">
              <AlertTriangle className="mr-2 h-4 w-4" />
              Failed to load site visit data.
            </div>
          )}

          {!isLoading && !isError && rows.length === 0 && (
            <div className="py-16 text-center text-sm text-muted-foreground">
              No site visits recorded yet.
              <p className="mt-1 text-xs">
                Visits are logged when the AI agent books a site visit during a call.
              </p>
            </div>
          )}

          {!isLoading && !isError && rows.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-sm" aria-label="Site visits by project">
                <thead className="border-b text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="px-4 py-2 font-medium">Project</th>
                    <th className="px-4 py-2 font-medium">Visits</th>
                    <th className="px-4 py-2 font-medium">Status</th>
                    <th className="px-4 py-2 font-medium">Share</th>
                  </tr>
                </thead>
                <tbody>
                  {rows
                    .slice()
                    .sort((a, b) => b.count - a.count)
                    .map((row) => (
                      <tr key={row.dimension} className="border-b last:border-b-0 hover:bg-muted/30">
                        <td className="px-4 py-3 font-medium">
                          {row.dimension === 'unassigned' ? (
                            <span className="text-muted-foreground italic">unassigned</span>
                          ) : (
                            row.dimension
                          )}
                        </td>
                        <td className="px-4 py-3 tabular-nums">{row.count}</td>
                        <td className="px-4 py-3">
                          <span className={stateClass(row.count)}>
                            {row.count === 0 ? 'none' : row.count < 3 ? 'low' : 'active'}
                          </span>
                        </td>
                        <td className="px-4 py-3">
                          <div className="flex items-center gap-2">
                            <div className="h-1.5 w-20 overflow-hidden rounded-full bg-muted">
                              <div
                                className="h-full rounded-full bg-primary"
                                style={{
                                  width:
                                    totalVisits > 0
                                      ? `${Math.round((row.count / totalVisits) * 100)}%`
                                      : '0%',
                                }}
                              />
                            </div>
                            <span className="text-xs tabular-nums text-muted-foreground">
                              {totalVisits > 0
                                ? `${Math.round((row.count / totalVisits) * 100)}%`
                                : '—'}
                            </span>
                          </div>
                        </td>
                      </tr>
                    ))}
                </tbody>
              </table>
            </div>
          )}
        </section>

        {/* Sidebar */}
        <aside className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-semibold">About Site Visits</h2>
            <p className="mt-2 text-sm text-muted-foreground">
              Site visits are booked by the AI agent during a call when a lead expresses
              interest. The data is grouped by project ID from ClickHouse.
            </p>
            <p className="mt-2 text-sm text-muted-foreground">
              The{' '}
              <code className="rounded bg-muted px-1 py-0.5 text-xs">site_visit.requested</code>{' '}
              event triggers the site-visit service and is counted here.
            </p>
          </section>

          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-semibold">Reschedule</h2>
            <div className="mt-3 flex items-center gap-2 text-sm text-muted-foreground">
              <span className="rounded-md border px-2 py-1">Visit card</span>
              <MoveRight className="h-4 w-4" />
              <span className="rounded-md border px-2 py-1">New date</span>
            </div>
            <p className="mt-2 text-xs text-muted-foreground">
              Full drag-and-drop calendar coming in Phase 7.
            </p>
          </section>
        </aside>
      </div>
    </div>
  );
}
