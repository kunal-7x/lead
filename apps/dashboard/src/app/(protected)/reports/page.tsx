'use client';

import { useState } from 'react';
import { BarChart3, CalendarRange, ChevronRight, Filter } from 'lucide-react';

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

const rows = [
  { dimension: 'Skyline Residency', count: 1240, value: 'Rs 4,820' },
  { dimension: 'Lakeview Towers', count: 980, value: 'Rs 3,110' },
  { dimension: 'Green Acres', count: 744, value: 'Rs 2,570' },
];

export default function ReportsPage() {
  const [active, setActive] = useState('campaign');

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
            tenant scoped - freshness 8s
          </span>
        </div>
        <div className="divide-y">
          {rows.map((row) => (
            <button
              key={row.dimension}
              className="grid w-full grid-cols-[1fr_140px_140px_32px] items-center px-4 py-3 text-left text-sm hover:bg-muted/40"
            >
              <span className="font-medium">{row.dimension}</span>
              <span className="text-muted-foreground">{row.count}</span>
              <span>{row.value}</span>
              <ChevronRight className="h-4 w-4 text-muted-foreground" />
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}
