'use client';

import Link from 'next/link';
import { CalendarDays, ChevronLeft, ChevronRight, Clock, MoveRight } from 'lucide-react';

const visits = Array.from({ length: 200 }, (_, index) => {
  const day = index % 7;
  const hour = 9 + (index % 9);
  const state = index % 11 === 0 ? 'no_show' : index % 5 === 0 ? 'completed' : 'confirmed';
  return {
    id: `visit-${index + 1}`,
    lead: ['Asha Mehta', 'Rohan Shah', 'Neha Iyer', 'Vikram Rao'][index % 4],
    project: ['Skyline Residency', 'Lakeview Towers', 'Green Acres'][index % 3],
    day,
    hour,
    state,
    rep: ['Priya', 'Karan', 'Meera'][index % 3],
  };
});

const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

export default function SiteVisitsPage() {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Site Visits</h1>
          <p className="text-sm text-muted-foreground">
            Week calendar, day queue, and reschedule actions.
          </p>
        </div>
        <div className="flex gap-2">
          <Link
            href="/site-visits/no-shows"
            className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
          >
            No-shows
          </Link>
          <button className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90">
            <CalendarDays className="h-4 w-4" />
            New visit
          </button>
        </div>
      </div>

      <div className="flex items-center justify-between rounded-lg border bg-card px-4 py-3">
        <button
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border hover:bg-muted"
          aria-label="Previous week"
        >
          <ChevronLeft className="h-4 w-4" />
        </button>
        <div className="text-sm font-medium">May 18 - May 24, 2026</div>
        <button
          className="inline-flex h-9 w-9 items-center justify-center rounded-md border hover:bg-muted"
          aria-label="Next week"
        >
          <ChevronRight className="h-4 w-4" />
        </button>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
        <section className="overflow-hidden rounded-lg border bg-card">
          <div className="grid grid-cols-7 border-b bg-muted/40 text-xs font-medium text-muted-foreground">
            {days.map((day) => (
              <div key={day} className="border-r px-3 py-2 last:border-r-0">
                {day}
              </div>
            ))}
          </div>
          <div className="grid h-[680px] grid-cols-7 overflow-y-auto">
            {days.map((day, dayIndex) => (
              <div key={day} className="space-y-2 border-r p-2 last:border-r-0">
                {visits
                  .filter((visit) => visit.day === dayIndex)
                  .slice(0, 30)
                  .map((visit) => (
                    <Link
                      key={visit.id}
                      href={`/site-visits/${visit.id}`}
                      draggable
                      className="block rounded-md border bg-background p-2 text-xs shadow-sm hover:border-primary"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-medium">{visit.hour}:00</span>
                        <span className={stateClass(visit.state)}>
                          {visit.state.replace('_', ' ')}
                        </span>
                      </div>
                      <p className="mt-1 truncate font-medium">{visit.lead}</p>
                      <p className="truncate text-muted-foreground">{visit.project}</p>
                    </Link>
                  ))}
              </div>
            ))}
          </div>
        </section>

        <aside className="space-y-4">
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-semibold">Day View</h2>
            <div className="mt-3 space-y-2">
              {visits.slice(0, 8).map((visit) => (
                <Link
                  key={visit.id}
                  href={`/site-visits/${visit.id}`}
                  className="flex items-center gap-3 rounded-md border p-3 text-sm hover:bg-muted/50"
                >
                  <Clock className="h-4 w-4 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-medium">{visit.lead}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {visit.hour}:00 with {visit.rep}
                    </p>
                  </div>
                </Link>
              ))}
            </div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-semibold">Drag Reschedule</h2>
            <div className="mt-3 flex items-center gap-2 text-sm text-muted-foreground">
              <span className="rounded-md border px-2 py-1">Visit card</span>
              <MoveRight className="h-4 w-4" />
              <span className="rounded-md border px-2 py-1">New day</span>
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}

function stateClass(state: string) {
  switch (state) {
    case 'confirmed':
      return 'rounded-full bg-blue-50 px-2 py-0.5 text-[11px] text-blue-700';
    case 'completed':
      return 'rounded-full bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700';
    case 'no_show':
      return 'rounded-full bg-red-50 px-2 py-0.5 text-[11px] text-red-700';
    default:
      return 'rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground';
  }
}
