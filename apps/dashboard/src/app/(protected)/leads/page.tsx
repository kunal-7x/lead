"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { api } from "@/lib/api";

interface Lead {
  id: string;
  contact_id: string;
  source_id?: string;
  status: string;
  score: number;
  assigned_to?: string;
  created_at: string;
}

async function fetchLeads(filters: Record<string, string>) {
  const params = new URLSearchParams(
    Object.fromEntries(Object.entries(filters).filter(([, v]) => v))
  );
  const { data } = await api.get(`/v1/leads?${params}`);
  return (data.leads ?? []) as Lead[];
}

const STATUSES = ["", "new", "contacted", "qualified", "converted", "lost"];

export default function LeadsPage() {
  const [status, setStatus] = useState("");
  const [assignedTo, setAssignedTo] = useState("");

  const { data: leads = [], isLoading } = useQuery({
    queryKey: ["leads", status, assignedTo],
    queryFn: () => fetchLeads({ status, assigned_to: assignedTo }),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Leads</h1>
        <Link
          href="/leads/import"
          className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Import
        </Link>
      </div>

      {/* Filters */}
      <div className="flex gap-3 rounded-lg border bg-card p-3">
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="rounded-md border px-3 py-1.5 text-sm"
        >
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {s ? s.charAt(0).toUpperCase() + s.slice(1) : "All statuses"}
            </option>
          ))}
        </select>
        <input
          type="text"
          placeholder="Assigned to (user ID)"
          value={assignedTo}
          onChange={(e) => setAssignedTo(e.target.value)}
          className="rounded-md border px-3 py-1.5 text-sm"
        />
      </div>

      {/* Table */}
      <div className="rounded-lg border bg-card">
        {isLoading ? (
          <div className="p-8 text-center text-muted-foreground">Loading…</div>
        ) : leads.length === 0 ? (
          <div className="p-8 text-center text-muted-foreground">
            No leads found.{" "}
            <Link href="/leads/import" className="underline">
              Import leads
            </Link>
          </div>
        ) : (
          <table className="w-full text-sm">
            <thead className="border-b bg-muted/50">
              <tr>
                <th className="px-4 py-3 text-left font-medium">Contact ID</th>
                <th className="px-4 py-3 text-left font-medium">Status</th>
                <th className="px-4 py-3 text-left font-medium">Score</th>
                <th className="px-4 py-3 text-left font-medium">Source</th>
                <th className="px-4 py-3 text-left font-medium">Created</th>
                <th className="px-4 py-3 text-left font-medium"></th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {leads.map((lead) => (
                <tr key={lead.id} className="hover:bg-muted/30">
                  <td className="px-4 py-3 font-mono text-xs">{lead.contact_id.slice(0, 8)}…</td>
                  <td className="px-4 py-3">
                    <span className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${statusColor(lead.status)}`}>
                      {lead.status}
                    </span>
                  </td>
                  <td className="px-4 py-3">{lead.score}</td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {lead.source_id ? lead.source_id.slice(0, 8) + "…" : "—"}
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {new Date(lead.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3">
                    <Link
                      href={`/leads/${lead.id}`}
                      className="text-primary hover:underline"
                    >
                      View
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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
