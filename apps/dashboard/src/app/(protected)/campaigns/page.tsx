'use client';

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Pause, Play, Plus, RefreshCw, Send } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/lib/api';

type CampaignStatus = 'draft' | 'active' | 'paused' | 'archived' | 'preflight_failed';

interface Campaign {
  id: string;
  name: string;
  project_id: string;
  tenant_id: string;
  kb_version_id: string;
  script_version_id: string;
  prompt_version_id: string;
  source_filter?: string;
  schedule?: string;
  status: CampaignStatus;
  pause_reason?: string;
  created_at?: string;
  updated_at?: string;
}

interface CampaignHealth {
  campaign_id: string;
  connect_rate: number;
  qualify_rate: number;
  cost_burn_inr: number;
  suppression_hit_rate: number;
}

const DEMO_TENANT_ID = 'tenant-demo';

const emptyCampaign = {
  name: 'Demo 10-lead campaign',
  project_id: 'project-skyline',
  kb_version_id: 'kb-demo-approved-1',
  script_version_id: 'script-demo-v1',
  prompt_version_id: 'prompt-demo-v1',
  source_filter: 'demo-upload',
  schedule: '09:00-21:00 Asia/Kolkata',
};

async function fetchCampaigns() {
  const { data } = await api.get(`/v1/campaigns?tenant_id=${DEMO_TENANT_ID}`);
  return Array.isArray(data) ? (data as Campaign[]) : ((data.campaigns ?? []) as Campaign[]);
}

async function fetchHealth(campaignID: string) {
  const { data } = await api.get(`/v1/campaigns/${campaignID}/health`);
  return data as CampaignHealth;
}

export default function CampaignsPage() {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState(emptyCampaign);
  const [leadIDs, setLeadIDs] = useState('lead-demo-001\nlead-demo-002\nlead-demo-003');
  const [selectedCampaignID, setSelectedCampaignID] = useState('');
  const [healthCampaignID, setHealthCampaignID] = useState('');

  const { data: campaigns = [], isLoading } = useQuery({
    queryKey: ['campaigns'],
    queryFn: fetchCampaigns,
  });

  const selectedCampaign = useMemo(
    () => campaigns.find((campaign) => campaign.id === selectedCampaignID) ?? campaigns[0],
    [campaigns, selectedCampaignID],
  );

  const healthQuery = useQuery({
    queryKey: ['campaign-health', healthCampaignID],
    queryFn: () => fetchHealth(healthCampaignID),
    enabled: Boolean(healthCampaignID),
  });

  const createMutation = useMutation({
    mutationFn: async () => {
      const { data } = await api.post('/v1/campaigns', {
        ...draft,
        tenant_id: DEMO_TENANT_ID,
        status: 'draft',
      });
      return data as Campaign;
    },
    onSuccess: (campaign) => {
      toast.success('Campaign created');
      setSelectedCampaignID(campaign.id);
      queryClient.invalidateQueries({ queryKey: ['campaigns'] });
    },
    onError: () => toast.error('Campaign create failed'),
  });

  const attachMutation = useMutation({
    mutationFn: async () => {
      if (!selectedCampaign) throw new Error('select campaign');
      const ids = leadIDs
        .split(/\r?\n|,/)
        .map((id) => id.trim())
        .filter(Boolean);
      const { data } = await api.post(`/v1/campaigns/${selectedCampaign.id}/leads`, {
        lead_ids: ids,
      });
      return data as { attached: number };
    },
    onSuccess: (data) => toast.success(`Attached ${data.attached ?? 0} leads`),
    onError: () => toast.error('Attach leads failed'),
  });

  const actionMutation = useMutation({
    mutationFn: async ({ id, action }: { id: string; action: 'launch' | 'pause' | 'resume' }) => {
      const body = action === 'pause' ? { reason: 'demo operator pause' } : {};
      const { data } = await api.post(`/v1/campaigns/${id}/${action}`, body);
      return data;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['campaigns'] });
      toast.success('Campaign updated');
    },
    onError: () => toast.error('Campaign action failed'),
  });

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">Campaigns</h1>
          <p className="text-sm text-muted-foreground">
            Launch demo calling flows against imported leads.
          </p>
        </div>
        <button
          type="button"
          onClick={() => queryClient.invalidateQueries({ queryKey: ['campaigns'] })}
          className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted"
        >
          <RefreshCw className="h-4 w-4" />
          Refresh
        </button>
      </div>

      <div className="grid gap-4 xl:grid-cols-[380px_1fr]">
        <section className="space-y-4 rounded-lg border bg-card p-4">
          <h2 className="text-base font-semibold">Create Campaign</h2>
          <div className="grid gap-3">
            <Field
              label="Name"
              value={draft.name}
              onChange={(value) => setDraft((current) => ({ ...current, name: value }))}
            />
            <Field
              label="Project"
              value={draft.project_id}
              onChange={(value) => setDraft((current) => ({ ...current, project_id: value }))}
            />
            <Field
              label="KB version"
              value={draft.kb_version_id}
              onChange={(value) => setDraft((current) => ({ ...current, kb_version_id: value }))}
            />
            <Field
              label="Script"
              value={draft.script_version_id}
              onChange={(value) =>
                setDraft((current) => ({ ...current, script_version_id: value }))
              }
            />
            <Field
              label="Prompt"
              value={draft.prompt_version_id}
              onChange={(value) =>
                setDraft((current) => ({ ...current, prompt_version_id: value }))
              }
            />
            <Field
              label="Schedule"
              value={draft.schedule}
              onChange={(value) => setDraft((current) => ({ ...current, schedule: value }))}
            />
          </div>
          <button
            type="button"
            onClick={() => createMutation.mutate()}
            disabled={createMutation.isPending}
            className="inline-flex w-full items-center justify-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            <Plus className="h-4 w-4" />
            {createMutation.isPending ? 'Creating...' : 'Create'}
          </button>
        </section>

        <section className="overflow-hidden rounded-lg border bg-card">
          <div className="border-b px-4 py-3">
            <h2 className="text-base font-semibold">Campaign Queue</h2>
          </div>
          {isLoading ? (
            <div className="p-8 text-center text-sm text-muted-foreground">Loading...</div>
          ) : campaigns.length === 0 ? (
            <div className="p-8 text-center text-sm text-muted-foreground">No campaigns found.</div>
          ) : (
            <div className="divide-y">
              {campaigns.map((campaign) => (
                <button
                  key={campaign.id}
                  type="button"
                  onClick={() => setSelectedCampaignID(campaign.id)}
                  className={`grid w-full gap-2 px-4 py-3 text-left hover:bg-muted/40 ${
                    selectedCampaign?.id === campaign.id ? 'bg-muted/60' : ''
                  }`}
                >
                  <span className="flex flex-wrap items-center justify-between gap-3">
                    <span className="font-medium">{campaign.name}</span>
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-medium ${statusColor(
                        campaign.status,
                      )}`}
                    >
                      {campaign.status}
                    </span>
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {campaign.project_id} - {campaign.kb_version_id}
                  </span>
                </button>
              ))}
            </div>
          )}
        </section>
      </div>

      <div className="grid gap-4 xl:grid-cols-[1fr_360px]">
        <section className="rounded-lg border bg-card p-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-base font-semibold">Launch Controls</h2>
              <p className="text-xs text-muted-foreground">
                {selectedCampaign ? selectedCampaign.name : 'Select a campaign'}
              </p>
            </div>
            {selectedCampaign ? (
              <span className="font-mono text-xs text-muted-foreground">{selectedCampaign.id}</span>
            ) : null}
          </div>

          <label className="grid gap-2 text-sm font-medium">
            Lead IDs
            <textarea
              value={leadIDs}
              onChange={(event) => setLeadIDs(event.target.value)}
              rows={5}
              className="min-h-28 rounded-md border bg-background px-3 py-2 font-mono text-xs outline-none focus:ring-2 focus:ring-ring"
            />
          </label>

          <div className="mt-4 flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => attachMutation.mutate()}
              disabled={!selectedCampaign || attachMutation.isPending}
              className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50"
            >
              <Send className="h-4 w-4" />
              Attach
            </button>
            <button
              type="button"
              onClick={() =>
                selectedCampaign &&
                actionMutation.mutate({ id: selectedCampaign.id, action: 'launch' })
              }
              disabled={!selectedCampaign || actionMutation.isPending}
              className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              <Play className="h-4 w-4" />
              Launch
            </button>
            <button
              type="button"
              onClick={() =>
                selectedCampaign &&
                actionMutation.mutate({ id: selectedCampaign.id, action: 'pause' })
              }
              disabled={!selectedCampaign || actionMutation.isPending}
              className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50"
            >
              <Pause className="h-4 w-4" />
              Pause
            </button>
            <button
              type="button"
              onClick={() =>
                selectedCampaign &&
                actionMutation.mutate({ id: selectedCampaign.id, action: 'resume' })
              }
              disabled={!selectedCampaign || actionMutation.isPending}
              className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50"
            >
              <Play className="h-4 w-4" />
              Resume
            </button>
            <button
              type="button"
              onClick={() => selectedCampaign && setHealthCampaignID(selectedCampaign.id)}
              disabled={!selectedCampaign}
              className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50"
            >
              <RefreshCw className="h-4 w-4" />
              Health
            </button>
          </div>
        </section>

        <section className="rounded-lg border bg-card p-4">
          <h2 className="text-base font-semibold">Health</h2>
          {healthQuery.isFetching ? (
            <p className="mt-4 text-sm text-muted-foreground">Loading...</p>
          ) : healthQuery.data ? (
            <dl className="mt-4 grid gap-3 text-sm">
              <Metric
                label="Connect"
                value={`${Math.round(healthQuery.data.connect_rate * 100)}%`}
              />
              <Metric
                label="Qualify"
                value={`${Math.round(healthQuery.data.qualify_rate * 100)}%`}
              />
              <Metric label="Cost" value={`₹${healthQuery.data.cost_burn_inr.toFixed(2)}`} />
              <Metric
                label="Suppression"
                value={`${Math.round(healthQuery.data.suppression_hit_rate * 100)}%`}
              />
            </dl>
          ) : (
            <p className="mt-4 text-sm text-muted-foreground">No health snapshot selected.</p>
          )}
        </section>
      </div>
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="grid gap-1 text-sm font-medium">
      {label}
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 rounded-md border bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
      />
    </label>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between rounded-md border px-3 py-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-semibold">{value}</dd>
    </div>
  );
}

function statusColor(status: CampaignStatus) {
  switch (status) {
    case 'active':
      return 'bg-green-100 text-green-700';
    case 'paused':
      return 'bg-yellow-100 text-yellow-700';
    case 'preflight_failed':
      return 'bg-red-100 text-red-700';
    case 'archived':
      return 'bg-muted text-muted-foreground';
    default:
      return 'bg-blue-100 text-blue-700';
  }
}
