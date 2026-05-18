import Link from 'next/link';
import { CalendarClock, CheckCircle2, RotateCw, XCircle } from 'lucide-react';

const events = [
  { type: 'visit_tentative_created', time: 'May 18, 09:10' },
  { type: 'visit_confirmed', time: 'May 18, 09:16' },
  { type: 'pre_visit_whatsapp_24h', time: 'May 19, 10:00' },
];

export default async function SiteVisitDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Site Visit Detail</h1>
          <p className="text-sm text-muted-foreground">{id} - Skyline Residency - Asha Mehta</p>
        </div>
        <Link
          href="/site-visits"
          className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
        >
          Calendar
        </Link>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
        <section className="rounded-lg border bg-card p-5">
          <div className="flex items-center gap-3">
            <CalendarClock className="h-5 w-5 text-muted-foreground" />
            <div>
              <h2 className="text-lg font-semibold">May 20, 2026 at 10:00 AM</h2>
              <p className="text-sm text-muted-foreground">Confirmed with Priya Sharma</p>
            </div>
          </div>

          <div className="mt-6 grid gap-3 sm:grid-cols-3">
            <button className="inline-flex items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground">
              <CheckCircle2 className="h-4 w-4" />
              Complete
            </button>
            <button className="inline-flex items-center justify-center gap-2 rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted">
              <RotateCw className="h-4 w-4" />
              Reschedule
            </button>
            <button className="inline-flex items-center justify-center gap-2 rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted">
              <XCircle className="h-4 w-4" />
              No-show
            </button>
          </div>
        </section>

        <aside className="rounded-lg border bg-card">
          <div className="border-b px-4 py-3">
            <h2 className="text-sm font-semibold">Event Log</h2>
          </div>
          <div className="divide-y">
            {events.map((event) => (
              <div key={event.type} className="px-4 py-3">
                <p className="text-sm font-medium">{event.type}</p>
                <p className="text-xs text-muted-foreground">{event.time}</p>
              </div>
            ))}
          </div>
        </aside>
      </div>
    </div>
  );
}
