import type { Metadata } from 'next';
import { RotateCcw } from 'lucide-react';
import { failedJobs } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Jobs - Capsy Admin' };

export default function JobsPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-normal">Failed Jobs</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          NATS DLQ inspector and Temporal failed-workflow viewer.
        </p>
      </div>
      <section className="rounded-lg border bg-card p-4">
        <div className="overflow-x-auto">
          <table className="w-full min-w-[820px] text-left text-sm" aria-label="Failed jobs table">
            <thead className="border-b text-xs uppercase text-muted-foreground">
              <tr>
                <th className="py-2 pr-3 font-medium">ID</th>
                <th className="py-2 pr-3 font-medium">Queue</th>
                <th className="py-2 pr-3 font-medium">Subject</th>
                <th className="py-2 pr-3 font-medium">Tenant</th>
                <th className="py-2 pr-3 font-medium">Workflow</th>
                <th className="py-2 pr-3 font-medium">Attempts</th>
                <th className="py-2 font-medium">Action</th>
              </tr>
            </thead>
            <tbody>
              {failedJobs.map((job) => (
                <tr key={job.id} className="border-b last:border-b-0">
                  <td className="py-3 pr-3 font-medium">{job.id}</td>
                  <td className="py-3 pr-3">{job.queue}</td>
                  <td className="py-3 pr-3">{job.subject}</td>
                  <td className="py-3 pr-3">{job.tenant}</td>
                  <td className="py-3 pr-3">{job.workflow}</td>
                  <td className="py-3 pr-3">{job.attempts}</td>
                  <td className="py-3">
                    <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs">
                      <RotateCcw className="h-3 w-3" aria-hidden="true" />
                      Replay
                    </button>
                    <div className="mt-1 text-xs text-muted-foreground">{job.error}</div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
