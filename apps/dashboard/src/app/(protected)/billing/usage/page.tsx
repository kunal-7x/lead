import Link from 'next/link';
import { BarChart3, IndianRupee } from 'lucide-react';

const rows = [
  { source: 'Calls', usage: '1,240 min', cost: 'Rs 1,488.00' },
  { source: 'WhatsApp', usage: '8,920 messages', cost: 'Rs 3,122.00' },
  { source: 'STT', usage: '74,400 sec', cost: 'Rs 297.60' },
  { source: 'TTS', usage: '2.1M chars', cost: 'Rs 168.00' },
  { source: 'LLM', usage: '4.8M tokens', cost: 'Rs 2,880.00' },
];

export default function BillingUsagePage() {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Usage</h1>
          <p className="text-sm text-muted-foreground">Tenant usage and cost by provider event.</p>
        </div>
        <BillingLinks />
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        {[
          ['Month to date', 'Rs 7,955.60'],
          ['Credits balance', 'Rs 42,044.40'],
          ['Cap usage', '79.5%'],
        ].map(([label, value]) => (
          <section key={label} className="rounded-lg border bg-card p-4">
            <p className="text-sm text-muted-foreground">{label}</p>
            <p className="mt-1 text-2xl font-semibold">{value}</p>
          </section>
        ))}
      </div>

      <section className="rounded-lg border bg-card">
        <div className="border-b px-4 py-3">
          <h2 className="inline-flex items-center gap-2 text-sm font-semibold">
            <BarChart3 className="h-4 w-4" />
            Metered Events
          </h2>
        </div>
        <div className="divide-y">
          {rows.map((row) => (
            <div
              key={row.source}
              className="grid grid-cols-[1fr_180px_160px] items-center px-4 py-3 text-sm"
            >
              <span className="font-medium">{row.source}</span>
              <span className="text-muted-foreground">{row.usage}</span>
              <span className="inline-flex items-center gap-1 font-medium">
                <IndianRupee className="h-3.5 w-3.5" />
                {row.cost.replace('Rs ', '')}
              </span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function BillingLinks() {
  return (
    <div className="flex flex-wrap gap-2">
      <Link
        href="/billing/caps"
        className="rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted"
      >
        Caps
      </Link>
      <Link
        href="/billing/credits"
        className="rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted"
      >
        Credits
      </Link>
      <Link
        href="/billing/invoices"
        className="rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted"
      >
        Invoices
      </Link>
    </div>
  );
}
