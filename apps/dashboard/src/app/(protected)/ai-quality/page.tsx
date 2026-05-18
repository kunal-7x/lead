import type { Metadata } from 'next';
import {
  AlertTriangle,
  CheckCircle2,
  FileJson,
  GitCompare,
  MessageSquareText,
  Save,
} from 'lucide-react';
import { promptRuns, qualityQueue } from '@/lib/ai-quality-data';

export const metadata: Metadata = { title: 'AI Quality - Capsy Dashboard' };

export default function AIQualityPage() {
  const selected = qualityQueue[0]!;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">AI Quality</h1>
          <p className="text-sm text-muted-foreground">
            Review low-confidence AI turns, hallucination incidents, and prompt regression gates.
          </p>
        </div>
        <button className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground">
          <Save className="h-4 w-4" />
          Accept correction
        </button>
      </div>

      <section className="grid gap-4 xl:grid-cols-[0.8fr_1.2fr]">
        <div className="rounded-lg border bg-card">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <h2 className="text-sm font-semibold">Human Review Queue</h2>
            <span className="rounded-md border px-2 py-1 text-xs text-muted-foreground">
              {qualityQueue.length} open
            </span>
          </div>
          <div className="divide-y">
            {qualityQueue.map((item) => (
              <button
                key={item.id}
                className="grid w-full gap-1 px-4 py-3 text-left text-sm hover:bg-muted/40"
              >
                <div className="flex items-center justify-between gap-3">
                  <span className="font-medium">{item.id}</span>
                  <span className="rounded-md border px-2 py-1 text-xs">{item.severity}</span>
                </div>
                <span className="text-muted-foreground">
                  {item.tenant} - {item.reason}
                </span>
                <span className="text-xs text-muted-foreground">
                  {item.session} - confidence {item.confidence}
                </span>
              </button>
            ))}
          </div>
        </div>

        <div className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <div className="mb-3 flex items-center justify-between gap-3">
              <h2 className="inline-flex items-center gap-2 text-sm font-semibold">
                <MessageSquareText className="h-4 w-4" />
                Reviewer Evidence
              </h2>
              <span className="text-xs text-muted-foreground">
                {selected.promptVersion} - {selected.modelVersion}
              </span>
            </div>
            <div className="grid gap-4 lg:grid-cols-2">
              <EvidenceList title="Transcript" rows={selected.transcript} />
              <EvidenceList title="KB chunks shown to LLM" rows={selected.kbChunks} />
            </div>
          </section>

          <section className="grid gap-4 lg:grid-cols-2">
            <div className="rounded-lg border bg-card p-4">
              <h2 className="mb-3 inline-flex items-center gap-2 text-sm font-semibold">
                <FileJson className="h-4 w-4" />
                Brain JSON
              </h2>
              <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">
                {JSON.stringify(selected.brainJson, null, 2)}
              </pre>
            </div>
            <div className="rounded-lg border bg-card p-4">
              <h2 className="mb-3 inline-flex items-center gap-2 text-sm font-semibold">
                <AlertTriangle className="h-4 w-4" />
                Hallucination Drill-in
              </h2>
              <dl className="space-y-2 text-sm">
                {selected.blameChain.map((row) => (
                  <div key={row.label} className="flex justify-between gap-4">
                    <dt className="text-muted-foreground">{row.label}</dt>
                    <dd className="font-medium">{row.value}</dd>
                  </div>
                ))}
              </dl>
            </div>
          </section>

          <section className="rounded-lg border bg-card p-4">
            <h2 className="mb-3 text-sm font-semibold">Reviewer Correction</h2>
            <div className="grid gap-3 lg:grid-cols-3">
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm"
                defaultValue="handover"
                aria-label="Correct next action"
              >
                <option value="handover">handover</option>
                <option value="book_site_visit">book_site_visit</option>
                <option value="callback">callback</option>
              </select>
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm"
                defaultValue="risky"
                aria-label="Correct risk level"
              >
                <option value="safe">safe</option>
                <option value="risky">risky</option>
                <option value="unsafe">unsafe</option>
              </select>
              <input
                className="h-10 rounded-md border px-3 text-sm"
                defaultValue="Possession timeline must be verified by sales"
                aria-label="Correct summary"
              />
            </div>
          </section>
        </div>
      </section>

      <section className="rounded-lg border bg-card p-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="inline-flex items-center gap-2 text-sm font-semibold">
            <GitCompare className="h-4 w-4" />
            Prompt Promotion Gate
          </h2>
          <span className="text-xs text-muted-foreground">active only after green suite run</span>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm" aria-label="Prompt test runs">
            <thead className="border-b text-xs uppercase text-muted-foreground">
              <tr>
                <th className="py-2 pr-3 font-medium">Run</th>
                <th className="py-2 pr-3 font-medium">Prompt</th>
                <th className="py-2 pr-3 font-medium">Model</th>
                <th className="py-2 pr-3 font-medium">Aggregate</th>
                <th className="py-2 pr-3 font-medium">High severity</th>
                <th className="py-2 font-medium">Status</th>
              </tr>
            </thead>
            <tbody>
              {promptRuns.map((run) => (
                <tr key={run.id} className="border-b last:border-b-0">
                  <td className="py-3 pr-3 font-medium">{run.id}</td>
                  <td className="py-3 pr-3">{run.prompt}</td>
                  <td className="py-3 pr-3">{run.model}</td>
                  <td className="py-3 pr-3">{run.aggregate}</td>
                  <td className="py-3 pr-3">{run.highSeverity}</td>
                  <td className="py-3">
                    <span className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs">
                      <CheckCircle2 className="h-3 w-3" />
                      {run.status}
                    </span>
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

function EvidenceList({ title, rows }: { title: string; rows: string[] }) {
  return (
    <div className="rounded-md border p-3">
      <h3 className="mb-2 text-xs font-semibold uppercase text-muted-foreground">{title}</h3>
      <div className="space-y-2">
        {rows.map((row) => (
          <div key={row} className="rounded-md bg-muted px-3 py-2 text-sm">
            {row}
          </div>
        ))}
      </div>
    </div>
  );
}
