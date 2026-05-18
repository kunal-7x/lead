import Link from 'next/link';
import { PhoneCall, RotateCw, Send } from 'lucide-react';

const noShows = [
  { id: 'visit-11', lead: 'Asha Mehta', project: 'Skyline Residency', attempts: 1 },
  { id: 'visit-22', lead: 'Rohan Shah', project: 'Lakeview Towers', attempts: 2 },
  { id: 'visit-33', lead: 'Neha Iyer', project: 'Green Acres', attempts: 3 },
];

export default function NoShowsPage() {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">No-show Recovery</h1>
          <p className="text-sm text-muted-foreground">
            Recovery workflow and loss reason capture.
          </p>
        </div>
        <Link
          href="/site-visits"
          className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
        >
          Calendar
        </Link>
      </div>

      <section className="rounded-lg border bg-card">
        <div className="grid grid-cols-[1fr_140px_260px] border-b bg-muted/40 px-4 py-2 text-xs font-medium text-muted-foreground">
          <span>Lead</span>
          <span>Attempts</span>
          <span>Actions</span>
        </div>
        <div className="divide-y">
          {noShows.map((visit) => (
            <div
              key={visit.id}
              className="grid grid-cols-[1fr_140px_260px] items-center gap-3 px-4 py-3"
            >
              <div className="min-w-0">
                <Link
                  href={`/site-visits/${visit.id}`}
                  className="text-sm font-medium hover:underline"
                >
                  {visit.lead}
                </Link>
                <p className="truncate text-xs text-muted-foreground">{visit.project}</p>
              </div>
              <span className="text-sm">{visit.attempts} / 3</span>
              <div className="flex gap-2">
                <button
                  className="inline-flex h-9 w-9 items-center justify-center rounded-md border hover:bg-muted"
                  aria-label="Send WhatsApp"
                >
                  <Send className="h-4 w-4" />
                </button>
                <button
                  className="inline-flex h-9 w-9 items-center justify-center rounded-md border hover:bg-muted"
                  aria-label="Call"
                >
                  <PhoneCall className="h-4 w-4" />
                </button>
                <button className="inline-flex items-center gap-2 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground">
                  <RotateCw className="h-4 w-4" />
                  Reschedule
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
