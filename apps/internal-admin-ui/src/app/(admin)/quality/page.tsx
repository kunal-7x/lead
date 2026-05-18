import type { Metadata } from 'next';
import { CheckCircle2, FileJson, MessageSquareWarning } from 'lucide-react';
import { qualityItems } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'AI Quality - Capsy Admin' };

export default function AIQualityPage() {
  const selected = qualityItems[0]!;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">AI Quality Queue</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Review low-confidence sessions, guardrail interventions, and hallucination incidents.
        </p>
      </div>

      <section className="grid gap-4 xl:grid-cols-[0.75fr_1.25fr]">
        <div className="rounded-lg border bg-card p-4">
          <h2 className="mb-3 text-base font-semibold">Review Queue</h2>
          <div className="grid gap-3">
            {qualityItems.map((item) => (
              <button
                key={item.id}
                className="flex flex-col gap-2 rounded-md border p-3 text-left hover:bg-accent"
              >
                <div className="flex items-start gap-3">
                  <MessageSquareWarning
                    className="mt-0.5 h-4 w-4 text-amber-700"
                    aria-hidden="true"
                  />
                  <div>
                    <div className="font-medium">{item.id}</div>
                    <div className="text-sm text-muted-foreground">
                      {item.tenant} - {item.session} - {item.reason}
                    </div>
                  </div>
                </div>
                <div className="text-xs text-muted-foreground">
                  {item.prompt} - {item.model} - KB {item.score}
                </div>
              </button>
            ))}
          </div>
        </div>

        <div className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <div className="mb-3 flex items-center justify-between gap-3">
              <h2 className="text-base font-semibold">{selected.id}</h2>
              <span className="rounded-md border px-2 py-1 text-xs">{selected.guardrail}</span>
            </div>
            <div className="grid gap-3 md:grid-cols-2">
              <Evidence title="Transcript" rows={selected.transcript} />
              <Evidence title="KB chunks" rows={selected.kb} />
            </div>
          </section>

          <section className="rounded-lg border bg-card p-4">
            <h2 className="mb-3 inline-flex items-center gap-2 text-base font-semibold">
              <FileJson className="h-4 w-4" aria-hidden="true" />
              Correction
            </h2>
            <div className="grid gap-3 md:grid-cols-3">
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm"
                defaultValue="handover"
                aria-label="Correct action"
              >
                <option value="handover">handover</option>
                <option value="book_site_visit">book_site_visit</option>
                <option value="callback">callback</option>
              </select>
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm"
                defaultValue="risky"
                aria-label="Correct risk"
              >
                <option value="safe">safe</option>
                <option value="risky">risky</option>
                <option value="unsafe">unsafe</option>
              </select>
              <button className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-primary px-3 text-sm text-primary-foreground">
                <CheckCircle2 className="h-4 w-4" aria-hidden="true" />
                Accept correction
              </button>
            </div>
          </section>
        </div>
      </section>
    </div>
  );
}

function Evidence({ title, rows }: { title: string; rows: string[] }) {
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
