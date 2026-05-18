import Link from 'next/link';

export default function BillingCapsPage() {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Cost Caps</h1>
          <p className="text-sm text-muted-foreground">
            80% alerts and 100% auto-pause guardrails.
          </p>
        </div>
        <Link
          href="/billing/usage"
          className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
        >
          Usage
        </Link>
      </div>

      <section className="rounded-lg border bg-card p-5">
        <div className="grid gap-4 sm:grid-cols-3">
          <label className="grid gap-1.5 text-sm font-medium">
            Tenant cap
            <input
              defaultValue="10000"
              className="h-10 rounded-md border px-3 text-sm font-normal"
            />
          </label>
          <label className="grid gap-1.5 text-sm font-medium">
            Campaign cap
            <input
              defaultValue="2500"
              className="h-10 rounded-md border px-3 text-sm font-normal"
            />
          </label>
          <label className="grid gap-1.5 text-sm font-medium">
            Max call seconds
            <input defaultValue="300" className="h-10 rounded-md border px-3 text-sm font-normal" />
          </label>
        </div>
        <button className="mt-5 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground">
          Save caps
        </button>
      </section>
    </div>
  );
}
