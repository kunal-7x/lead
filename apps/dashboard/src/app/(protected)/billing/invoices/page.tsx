import Link from 'next/link';

export default function BillingInvoicesPage() {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Invoices</h1>
          <p className="text-sm text-muted-foreground">
            Invoice export placeholder for billing close.
          </p>
        </div>
        <Link
          href="/billing/usage"
          className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
        >
          Usage
        </Link>
      </div>

      <section className="rounded-lg border bg-card p-8 text-center text-sm text-muted-foreground">
        Invoice generation will use monthly client cost summaries after finance approval.
      </section>
    </div>
  );
}
