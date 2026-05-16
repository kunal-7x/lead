import type { Metadata } from "next";

export const metadata: Metadata = { title: "Dashboard — EVS" };

const cards = [
  { title: "Active Campaigns", value: "—", desc: "Campaigns running now" },
  { title: "Leads Today", value: "—", desc: "Leads contacted today" },
  { title: "Conversion Rate", value: "—", desc: "Last 30 days" },
  { title: "API Usage", value: "—", desc: "Calls this month" },
];

export default function DashboardPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {cards.map((c) => (
          <div
            key={c.title}
            className="rounded-lg border bg-card p-5 shadow-sm"
          >
            <p className="text-sm font-medium text-muted-foreground">
              {c.title}
            </p>
            <p className="mt-1 text-3xl font-bold">{c.value}</p>
            <p className="mt-1 text-xs text-muted-foreground">{c.desc}</p>
          </div>
        ))}
      </div>
    </div>
  );
}
