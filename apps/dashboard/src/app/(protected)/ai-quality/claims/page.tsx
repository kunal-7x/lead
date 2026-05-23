import type { Metadata } from 'next';
import { Check, FileWarning, Plus, ShieldAlert } from 'lucide-react';

export const metadata: Metadata = { title: 'Claims - Capsy Dashboard' };

const claims = [
  {
    id: 'claim-price-1',
    type: 'price',
    status: 'allowed',
    pattern: 'Price starts at 1.5 crore',
    expiry: '30 Jun 2026',
  },
  {
    id: 'claim-rera-1',
    type: 'rera',
    status: 'needs_human_approval',
    pattern: 'RERA registered with HARERA Gurugram',
    expiry: 'Open',
  },
  {
    id: 'claim-roi-1',
    type: 'appreciation',
    status: 'forbidden',
    pattern: 'Guaranteed appreciation',
    expiry: 'Always',
  },
];

const violations = [
  {
    id: 'vio-1842',
    project: 'Skyline Gurugram',
    channel: 'voice',
    type: 'appreciation',
    action: 'rewritten',
    text: 'Guaranteed appreciation 20 percent yearly',
  },
  {
    id: 'vio-1841',
    project: 'Delhi Central Homes',
    channel: 'whatsapp',
    type: 'loan',
    action: 'blocked',
    text: 'Definite loan approval available',
  },
];

export default function ClaimsPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Claims</h1>
          <p className="text-sm text-muted-foreground">
            Project-approved wording for price, RERA, offers, possession, loan, and investment
            claims.
          </p>
        </div>
        <button className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground">
          <Plus className="h-4 w-4" />
          Add claim
        </button>
      </div>

      <section className="grid gap-4 xl:grid-cols-[0.85fr_1.15fr]">
        <div className="rounded-lg border bg-card p-4">
          <h2 className="mb-4 inline-flex items-center gap-2 text-sm font-semibold">
            <ShieldAlert className="h-4 w-4" />
            New Claim
          </h2>
          <div className="grid gap-3">
            <select
              className="h-10 rounded-md border bg-background px-3 text-sm"
              defaultValue="Skyline Gurugram"
            >
              <option>Skyline Gurugram</option>
              <option>Delhi Central Homes</option>
            </select>
            <div className="grid gap-3 sm:grid-cols-2">
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm"
                defaultValue="price"
              >
                <option value="price">price</option>
                <option value="rera">rera</option>
                <option value="possession">possession</option>
                <option value="loan">loan</option>
                <option value="appreciation">appreciation</option>
              </select>
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm"
                defaultValue="allowed"
              >
                <option value="allowed">allowed</option>
                <option value="forbidden">forbidden</option>
                <option value="needs_human_approval">needs_human_approval</option>
              </select>
            </div>
            <textarea
              className="min-h-24 rounded-md border bg-background px-3 py-2 text-sm"
              defaultValue="Price starts at 1.5 crore as per approved price sheet."
            />
            <input
              className="h-10 rounded-md border bg-background px-3 text-sm"
              defaultValue="Let me check that and get back to you."
            />
            <button className="inline-flex h-10 items-center justify-center gap-2 rounded-md border px-3 text-sm">
              <Check className="h-4 w-4" />
              Save for approval
            </button>
          </div>
        </div>

        <div className="rounded-lg border bg-card">
          <div className="flex items-center justify-between border-b px-4 py-3">
            <h2 className="text-sm font-semibold">Approved Claim Library</h2>
            <span className="rounded-md border px-2 py-1 text-xs text-muted-foreground">
              Pan-India + Gurugram + Delhi
            </span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-left text-sm">
              <thead className="border-b text-xs uppercase text-muted-foreground">
                <tr>
                  <th className="py-2 pl-4 pr-3 font-medium">Type</th>
                  <th className="py-2 pr-3 font-medium">Status</th>
                  <th className="py-2 pr-3 font-medium">Pattern</th>
                  <th className="py-2 pr-4 font-medium">Expiry</th>
                </tr>
              </thead>
              <tbody>
                {claims.map((claim) => (
                  <tr key={claim.id} className="border-b last:border-b-0">
                    <td className="py-3 pl-4 pr-3 font-medium">{claim.type}</td>
                    <td className="py-3 pr-3">
                      <span className="rounded-md border px-2 py-1 text-xs">{claim.status}</span>
                    </td>
                    <td className="py-3 pr-3">{claim.pattern}</td>
                    <td className="py-3 pr-4">{claim.expiry}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </section>

      <section className="rounded-lg border bg-card">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <h2 className="inline-flex items-center gap-2 text-sm font-semibold">
            <FileWarning className="h-4 w-4" />
            Violation History
          </h2>
          <span className="text-xs text-muted-foreground">latest blocks and rewrites</span>
        </div>
        <div className="divide-y">
          {violations.map((violation) => (
            <div
              key={violation.id}
              className="grid gap-2 px-4 py-3 text-sm md:grid-cols-[1fr_120px_120px_1.5fr]"
            >
              <div>
                <div className="font-medium">{violation.project}</div>
                <div className="text-xs text-muted-foreground">{violation.id}</div>
              </div>
              <div>{violation.channel}</div>
              <div>{violation.action}</div>
              <div className="text-muted-foreground">{violation.text}</div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
