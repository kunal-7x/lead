import type { Metadata } from 'next';
import { Save } from 'lucide-react';
import { featureFlags } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Feature Flags - Capsy Admin' };

export default function FeatureFlagsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">Feature Flags</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Per-tenant toggles ready for Unleash or GrowthBook integration.
        </p>
      </div>
      <section className="rounded-lg border bg-card p-4">
        <div className="mb-4 grid gap-3 md:grid-cols-[1fr_1fr_auto]">
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Flag reason"
            placeholder="Reason"
          />
          <input
            className="h-10 rounded-md border px-3 text-sm"
            aria-label="Flag ticket id"
            placeholder="Ticket id"
          />
          <button className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-3 text-sm text-primary-foreground">
            <Save className="h-4 w-4" aria-hidden="true" />
            Save changes
          </button>
        </div>
        <div className="grid gap-3">
          {featureFlags.map((flag) => (
            <label
              key={`${flag.tenant}-${flag.key}`}
              className="flex items-center justify-between gap-4 rounded-md border p-3"
            >
              <span>
                <span className="block text-sm font-medium">{flag.key}</span>
                <span className="block text-xs text-muted-foreground">
                  {flag.tenant} - {flag.description}
                </span>
              </span>
              <input
                className="h-5 w-5"
                type="checkbox"
                defaultChecked={flag.enabled}
                aria-label={`${flag.key} enabled`}
              />
            </label>
          ))}
        </div>
      </section>
    </div>
  );
}
