import Link from 'next/link';

const entries = [
  { reason: 'Opening credit', amount: '+ Rs 50,000.00' },
  { reason: 'Usage charge', amount: '- Rs 7,955.60' },
];

export default function BillingCreditsPage() {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Credits</h1>
          <p className="text-sm text-muted-foreground">Credits ledger and current balance.</p>
        </div>
        <Link
          href="/billing/usage"
          className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
        >
          Usage
        </Link>
      </div>

      <section className="rounded-lg border bg-card">
        <div className="border-b px-4 py-3 text-sm font-semibold">Ledger</div>
        <div className="divide-y">
          {entries.map((entry) => (
            <div key={entry.reason} className="flex items-center justify-between px-4 py-3 text-sm">
              <span>{entry.reason}</span>
              <span className="font-medium">{entry.amount}</span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
