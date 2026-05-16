"use client";

import { useQuery } from "@tanstack/react-query";
import { useParams, useRouter } from "next/navigation";
import { api } from "@/lib/api";

interface Lead {
  id: string;
  contact_id: string;
  source_id?: string;
  status: string;
  score: number;
  assigned_to?: string;
  created_at: string;
  updated_at: string;
}

interface Activity {
  id: string;
  lead_id: string;
  type: string;
  payload?: Record<string, unknown>;
  actor_id?: string;
  occurred_at: string;
}

interface StatusHistory {
  id: string;
  old_status?: string;
  new_status: string;
  actor_id?: string;
  changed_at: string;
}

async function fetchLead(id: string) {
  const { data } = await api.get(`/v1/leads/${id}`);
  return data.lead as Lead;
}

async function fetchActivities(id: string) {
  try {
    const { data } = await api.get(`/v1/leads/${id}/activities`);
    return (data.activities ?? []) as Activity[];
  } catch {
    return [] as Activity[];
  }
}

async function fetchStatusHistory(id: string) {
  try {
    const { data } = await api.get(`/v1/leads/${id}/status-history`);
    return (data.history ?? []) as StatusHistory[];
  } catch {
    return [] as StatusHistory[];
  }
}

export default function LeadDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();

  const { data: lead, isLoading } = useQuery({
    queryKey: ["lead", id],
    queryFn: () => fetchLead(id),
  });

  const { data: activities = [] } = useQuery({
    queryKey: ["lead-activities", id],
    queryFn: () => fetchActivities(id),
  });

  const { data: history = [] } = useQuery({
    queryKey: ["lead-history", id],
    queryFn: () => fetchStatusHistory(id),
  });

  if (isLoading) {
    return <div className="p-8 text-muted-foreground">Loading…</div>;
  }

  if (!lead) {
    return (
      <div className="p-8 text-center">
        <p className="text-muted-foreground">Lead not found.</p>
        <button
          onClick={() => router.push("/leads")}
          className="mt-4 text-sm text-primary hover:underline"
        >
          Back to leads
        </button>
      </div>
    );
  }

  // Merge and sort timeline events
  const timeline = [
    ...activities.map((a) => ({
      id: a.id,
      type: a.type,
      detail: JSON.stringify(a.payload ?? {}),
      actor: a.actor_id,
      at: a.occurred_at,
    })),
    ...history.map((h) => ({
      id: h.id,
      type: "status_change",
      detail: `${h.old_status ?? "—"} → ${h.new_status}`,
      actor: h.actor_id,
      at: h.changed_at,
    })),
  ].sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime());

  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <div className="flex items-center gap-3">
        <button
          onClick={() => router.push("/leads")}
          className="text-sm text-muted-foreground hover:underline"
        >
          ← Leads
        </button>
        <h1 className="text-2xl font-bold">Lead detail</h1>
      </div>

      {/* Info card */}
      <div className="rounded-lg border bg-card p-6">
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-muted-foreground">ID</dt>
            <dd className="font-mono text-xs">{lead.id}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Contact</dt>
            <dd className="font-mono text-xs">{lead.contact_id}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Status</dt>
            <dd>
              <span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${statusColor(lead.status)}`}>
                {lead.status}
              </span>
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Score</dt>
            <dd>{lead.score}</dd>
          </div>
          {lead.assigned_to && (
            <div>
              <dt className="text-muted-foreground">Assigned to</dt>
              <dd className="font-mono text-xs">{lead.assigned_to}</dd>
            </div>
          )}
          <div>
            <dt className="text-muted-foreground">Created</dt>
            <dd>{new Date(lead.created_at).toLocaleString()}</dd>
          </div>
        </dl>
      </div>

      {/* Timeline */}
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">Timeline</h2>
        {timeline.length === 0 ? (
          <p className="text-sm text-muted-foreground">No activity yet.</p>
        ) : (
          <ol className="relative border-l border-border pl-6 space-y-4">
            {timeline.map((event) => (
              <li key={event.id} className="relative">
                <span className="absolute -left-[25px] flex h-4 w-4 items-center justify-center rounded-full border bg-background ring-2 ring-background">
                  <span className="h-1.5 w-1.5 rounded-full bg-primary" />
                </span>
                <div className="rounded-md border bg-card px-4 py-2">
                  <p className="text-sm font-medium capitalize">
                    {event.type.replace(/_/g, " ")}
                  </p>
                  {event.detail && (
                    <p className="text-xs text-muted-foreground">{event.detail}</p>
                  )}
                  <p className="mt-1 text-xs text-muted-foreground">
                    {new Date(event.at).toLocaleString()}
                    {event.actor && ` · ${event.actor}`}
                  </p>
                </div>
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  );
}

function statusColor(status: string) {
  switch (status) {
    case "new": return "bg-blue-100 text-blue-700";
    case "contacted": return "bg-yellow-100 text-yellow-700";
    case "qualified": return "bg-green-100 text-green-700";
    case "converted": return "bg-emerald-100 text-emerald-700";
    case "lost": return "bg-red-100 text-red-700";
    default: return "bg-muted text-muted-foreground";
  }
}
