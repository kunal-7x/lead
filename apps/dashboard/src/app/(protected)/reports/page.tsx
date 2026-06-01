'use client';

import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { BarChart3, CalendarRange, ChevronRight, Filter, Loader2 } from 'lucide-react';
import { api } from '@/lib/api';

const DEMO_TENANT_ID = 'tenant-demo';

const reports = [
  'daily',
  'monthly',
  'campaign',
  'source',
  'project',
  'salesperson',
  'site-visit',
  'whatsapp',
  'cost',
  'ai-quality',
];

type ReportRow = { dimension: string; count: number; value: number };
type ReportResponse = {
  tenant_id: string;
  report: string;
  freshness_s: number;
  rows: ReportRow[];
};

async function fetchReport(report: string): Promise<ReportResponse> {
  const { data } = await api.get(
    `/v1/reports/${encodeURIComponent(report)}?tenant_id=${DEMO_TENANT_ID}`,
  );
  return data;
}

function formatValue(value: number): string {
  return `Rs ${Math.round(value).toLocaleString('en-IN')}`;
}

export default function ReportsPage() {
  const [active, setActive] = useState('campaign');

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['report', active],
    queryFn: () => fetchReport(active),
  });

  const rows = data?.rows ?? [];
  const freshness = data?.freshness_s;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Reports</h1>
          <p className="text-sm text-muted-foreground">ClickHouse-backed operational reporting.</p>
        </div>
        <div className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
          <CalendarRange className="h-4 w-4" />
          Last 30 days
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        {reports.map((report) => (
          <button
            key={report}
            onClick={() => setActive(report)}
            className={`inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${
              active === report ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'
            }`}
          >
            {report}
          </button>
        ))}
      </div>

      <section className="rounded-lg border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="inline-flex items-center gap-2 text-sm font-semibold">
            <BarChart3 className="h-4 w-4" />
            {active} report
          </h2>
          <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
            <Filter className="h-3.5 w-3.5" />
            tenant scoped{typeof freshness === 'number' ? ` - freshness ${freshness}s` : ''}
          </span>
        </div>

        {isLoading ? (
          <div className="flex items-center justify-center gap-2 px-4 py-10 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            Loading {active} report...
          </div>
        ) : isError ? (
          <div className="px-4 py-10 text-center text-sm text-destructive">
            Failed to load report{error instanceof Error ? `: ${error.message}` : ''}.
          </div>
        ) : rows.length === 0 ? (
          <div className="px-4 py-10 text-center text-sm text-muted-foreground">
            No data yet for the {active} report. Data appears here as calls and events flow in.
          </div>
        ) : (
          <div className="divide-y">
            {rows.map((row) => (
              <button
                key={row.dimension}
                className="grid w-full grid-cols-[1fr_140px_140px_32px] items-center px-4 py-3 text-left text-sm hover:bg-muted/40"
              >
                <span className="font-medium">{row.dimension}</span>
                <span className="text-muted-foreground">{row.count}</span>
                <span>{formatValue(row.value)}</span>
                <ChevronRight className="h-4 w-4 text-muted-foreground" />
              </button>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
