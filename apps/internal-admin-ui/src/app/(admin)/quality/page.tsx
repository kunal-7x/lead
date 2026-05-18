import type { Metadata } from 'next';
import { CheckCircle2, MessageSquareWarning } from 'lucide-react';
import { qualityItems } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'AI Quality - Capsy Admin' };

export default function AIQualityPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">AI Quality Queue</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Review stub for low-confidence sessions and guardrail interventions.
        </p>
      </div>
      <section className="rounded-lg border bg-card p-4">
        <div className="grid gap-3">
          {qualityItems.map((item) => (
            <div
              key={item.id}
              className="flex flex-col gap-3 rounded-md border p-3 md:flex-row md:items-center md:justify-between"
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
              <button className="inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm">
                <CheckCircle2 className="h-4 w-4" aria-hidden="true" />
                Mark reviewed
              </button>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
