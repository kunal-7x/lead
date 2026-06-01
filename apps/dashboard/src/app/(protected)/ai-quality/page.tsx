'use client';

import { useQuery } from '@tanstack/react-query';
import {
  AlertTriangle,
  BarChart2,
  CheckCircle2,
  GitCompare,
  Loader2,
  MessageSquareText,
} from 'lucide-react';
import { api } from '@/lib/api';

const DEMO_TENANT_ID = 'tenant-demo';

type ReportRow = { dimension: string; count: number; value: number };
type ReportResponse = {
  tenant_id: string;
  report: string;
  freshness_s: number;
  rows: ReportRow[];
};

async function fetchQualityReport(): Promise<ReportResponse> {
  const { data } = await api.get(
    `/v1/reports/ai-quality?tenant_id=${DEMO_TENANT_ID}`,
  );
  return data;
}

function severityClass(value: number) {
  if (value < 0.4) return 'rounded-full bg-red-50 px-2 py-0.5 text-[11px] text-red-700';
  if (value < 0.7) return 'rounded-full bg-amber-50 px-2 py-0.5 text-[11px] text-amber-700';
  return 'rounded-full bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700';
}

function severityLabel(value: number) {
  if (value < 0.4) return 'high';
  if (value < 0.7) return 'medium';
  return 'low';
}

export default function AIQualityPage() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['report', 'ai-quality'],
    queryFn: fetchQualityReport,
  });

  const rows = data?.rows ?? [];
  const freshness = data?.freshness_s;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">AI Quality</h1>
          <p className="text-sm text-muted-foreground">
            Review low-confidence AI turns, hallucination incidents, and prompt regression gates.
          </p>
        </div>
        {freshness != null && (
          <span className="text-xs text-muted-foreground">
            Data freshness: {freshness}s ago
          </span>
        )}
      </div>

      <section className="grid gap-4 xl:grid-cols-[0.8fr_1.2fr]">
        <div className="rounded-lg border bg-card">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <h2 className="text-sm font-semibold">Quality by Session</h2>
            <span className="rounded-md border px-2 py-1 text-xs text-muted-foreground">
              {rows.length} sessions
            </span>
          </div>

          {isLoading && (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Loading quality data…
            </div>
          )}

          {isError && (
            <div className="flex items-center justify-center py-12 text-sm text-destructive">
              <AlertTriangle className="mr-2 h-4 w-4" />
              Failed to load quality data.
            </div>
          )}

          {!isLoading && !isError && rows.length === 0 && (
            <div className="py-12 text-center text-sm text-muted-foreground">
              No quality events recorded yet.
              <p className="mt-1 text-xs">Quality facts are written after each call completes.</p>
            </div>
          )}

          {!isLoading && !isError && rows.length > 0 && (
            <div className="divide-y">
              {rows.map((row) => (
                <div
                  key={row.dimension}
                  className="grid w-full gap-1 px-4 py-3 text-left text-sm"
                >
                  <div className="flex items-center justify-between gap-3">
                    <span className="truncate font-medium font-mono text-xs">{row.dimension}</span>
                    <span className={severityClass(row.value)}>
                      {severityLabel(row.value)}
                    </span>
                  </div>
                  <div className="flex items-center gap-3">
                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-primary transition-all"
                        style={{ width: `${Math.round(row.value * 100)}%` }}
                      />
                    </div>
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {(row.value * 100).toFixed(0)}% — {row.count} turns
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <div className="mb-3 flex items-center gap-2">
              <BarChart2 className="h-4 w-4" />
              <h2 className="text-sm font-semibold">Aggregate Scores</h2>
            </div>
            {isLoading && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" /> Loading…
              </div>
            )}
            {!isLoading && rows.length > 0 && (() => {
              const avg = rows.reduce((s, r) => s + r.value, 0) / rows.length;
              const totalTurns = rows.reduce((s, r) => s + r.count, 0);
              const lowConf = rows.filter((r) => r.value < 0.4).length;
              return (
                <dl className="grid grid-cols-3 gap-4">
                  <div className="rounded-md border p-3 text-center">
                    <dt className="text-xs text-muted-foreground">Avg score</dt>
                    <dd className="mt-1 text-2xl font-semibold">{(avg * 100).toFixed(0)}%</dd>
                  </div>
                  <div className="rounded-md border p-3 text-center">
                    <dt className="text-xs text-muted-foreground">Total turns</dt>
                    <dd className="mt-1 text-2xl font-semibold">{totalTurns}</dd>
                  </div>
                  <div className="rounded-md border p-3 text-center">
                    <dt className="text-xs text-muted-foreground">High risk</dt>
                    <dd className="mt-1 text-2xl font-semibold text-destructive">{lowConf}</dd>
                  </div>
                </dl>
              );
            })()}
            {!isLoading && rows.length === 0 && (
              <p className="text-sm text-muted-foreground">No data yet.</p>
            )}
          </section>

          <section className="rounded-lg border bg-card p-4">
            <h2 className="mb-3 inline-flex items-center gap-2 text-sm font-semibold">
              <MessageSquareText className="h-4 w-4" />
              About Quality Scoring
            </h2>
            <p className="text-sm text-muted-foreground">
              Quality scores are written to ClickHouse after each call via the{' '}
              <code className="rounded bg-muted px-1 py-0.5 text-xs">call.completed</code> event.
              Each session dimension is the Vobiz session ID. Score is the average confidence
              across all turns (0–1). Low-confidence sessions (below 40%) are flagged for review.
            </p>
          </section>

          <section className="rounded-lg border bg-card p-4">
            <h2 className="mb-3 inline-flex items-center gap-2 text-sm font-semibold">
              <GitCompare className="h-4 w-4" />
              Prompt Promotion Gate
            </h2>
            <div className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm text-muted-foreground">
              <CheckCircle2 className="h-4 w-4 text-emerald-600" />
              Active after eval-harness green suite run.
            </div>
          </section>
        </div>
      </section>
    </div>
  );
}
