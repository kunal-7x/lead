'use client';

import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Pause, Play, Plus, RefreshCw, Send, Sparkles } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/lib/api';

type CampaignStatus = 'draft' | 'active' | 'paused' | 'archived' | 'preflight_failed';

interface CampaignContext {
  product_description: string;
  offer: string;
  talking_points: string[];
  objection_handling: { objection: string; response: string }[];
  qualifying_questions: string[];
  persona: string;
  do_not_say: string[];
  goal: string;
  language: string;
  business_hours: string;
  // Sarvam bulbul:v3 speaker id. Drives the TTS voice + greeting grammar + LLM
  // persona gender for the WHOLE conversation (see VOICE_OPTIONS below).
  voice: string;
}

// Available agent voices (Sarvam bulbul:v3). The gender label tells the vendor
// the conversation's gender — picking a voice flips greeting grammar + persona.
const VOICE_OPTIONS: { id: string; label: string; gender: 'Male' | 'Female' }[] = [
  { id: 'rahul', label: 'Rahul', gender: 'Male' },
  { id: 'aditya', label: 'Aditya', gender: 'Male' },
  { id: 'rohan', label: 'Rohan', gender: 'Male' },
  { id: 'kabir', label: 'Kabir', gender: 'Male' },
  { id: 'priya', label: 'Priya', gender: 'Female' },
  { id: 'neha', label: 'Neha', gender: 'Female' },
  { id: 'pooja', label: 'Pooja', gender: 'Female' },
  { id: 'kavya', label: 'Kavya', gender: 'Female' },
  { id: 'simran', label: 'Simran', gender: 'Female' },
];

interface CampaignLimits {
  daily_call_cap: number;
  hourly_call_cap: number;
  concurrent_cap: number;
  retry_max: number;
  retry_busy_min: number;
  retry_no_answer_min: number;
  cost_cap_inr: number;
  max_call_seconds: number;
  call_window_start_hour: number;
  call_window_end_hour: number;
  timezone: string;
}

interface CampaignProgress {
  campaign_id: string;
  total: number;
  pending: number;
  in_flight: number;
  placed: number;
  connected: number;
  no_answer_retry: number;
  failed: number;
  suppressed: number;
  done: number;
}

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

const emptyContext: CampaignContext = {
  product_description: '',
  offer: '',
  talking_points: [],
  objection_handling: [],
  qualifying_questions: [],
  persona: '',
  do_not_say: [],
  goal: '',
  language: 'English',
  business_hours: '',
  voice: 'rahul',
};

const emptyLimits: CampaignLimits = {
  daily_call_cap: 500,
  hourly_call_cap: 50,
  concurrent_cap: 3,
  retry_max: 3,
  retry_busy_min: 10,
  retry_no_answer_min: 30,
  cost_cap_inr: 0,
  max_call_seconds: 300,
  call_window_start_hour: 10,
  call_window_end_hour: 19,
  timezone: 'Asia/Kolkata',
};

const emptyCampaign = {
  name: 'Demo 10-lead campaign',
  project_id: 'project-skyline',
  kb_version_id: 'kb-demo-approved-1',
  script_version_id: 'script-demo-v1',
  prompt_version_id: 'prompt-demo-v1',
  source_filter: 'demo-upload',
  schedule: '09:00-21:00 Asia/Kolkata',
};

// Helpers for array fields (split by comma or newline)
function splitLines(text: string): string[] {
  return text
    .split(/\r?\n|,/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function joinLines(arr: string[]): string {
  return arr.join('\n');
}

// objection_handling: each line is "objection :: response"
function parseObjections(text: string): { objection: string; response: string }[] {
  return text
    .split(/\r?\n/)
    .map((line) => {
      const [objection, ...rest] = line.split('::');
      return { objection: (objection ?? '').trim(), response: rest.join('::').trim() };
    })
    .filter((item) => item.objection);
}

function joinObjections(arr: { objection: string; response: string }[]): string {
  return arr.map((item) => `${item.objection} :: ${item.response}`).join('\n');
}

async function fetchCampaigns() {
  const { data } = await api.get(`/v1/campaigns?tenant_id=${DEMO_TENANT_ID}`);
  return Array.isArray(data) ? (data as Campaign[]) : ((data.campaigns ?? []) as Campaign[]);
}

async function fetchHealth(campaignID: string) {
  const { data } = await api.get(`/v1/campaigns/${campaignID}/health`);
  return data as CampaignHealth;
}

async function fetchProgress(campaignID: string) {
  const { data } = await api.get(`/v1/campaigns/${campaignID}/progress`);
  return data as CampaignProgress;
}

export default function CampaignsPage() {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState(emptyCampaign);
  const [context, setContext] = useState<CampaignContext>(emptyContext);
  const [limits, setLimits] = useState<CampaignLimits>(emptyLimits);
  const [briefText, setBriefText] = useState('');
  const [leadIDs, setLeadIDs] = useState('lead-demo-001\nlead-demo-002\nlead-demo-003');
  const [selectedCampaignID, setSelectedCampaignID] = useState('');
  const [healthCampaignID, setHealthCampaignID] = useState('');

  // Context textarea state (string form for UI, parsed on submit)
  const [talkingPointsText, setTalkingPointsText] = useState('');
  const [objectionText, setObjectionText] = useState('');
  const [qualifyingText, setQualifyingText] = useState('');
  const [doNotSayText, setDoNotSayText] = useState('');

  const { data: campaigns = [], isLoading } = useQuery({
    queryKey: ['campaigns'],
    queryFn: fetchCampaigns,
  });

  const selectedCampaign = useMemo(
    () => campaigns.find((campaign) => campaign.id === selectedCampaignID) ?? campaigns[0],
    [campaigns, selectedCampaignID],
  );

  const isActive = selectedCampaign?.status === 'active';

  const healthQuery = useQuery({
    queryKey: ['campaign-health', healthCampaignID],
    queryFn: () => fetchHealth(healthCampaignID),
    enabled: Boolean(healthCampaignID),
  });

  const progressQuery = useQuery({
    queryKey: ['campaign-progress', selectedCampaign?.id],
    queryFn: () => fetchProgress(selectedCampaign!.id),
    refetchInterval: 2000,
    enabled: isActive && Boolean(selectedCampaign?.id),
  });

  // AI extract + prefill
  const extractMutation = useMutation({
    mutationFn: async () => {
      const { data } = await api.post('/v1/campaigns/extract', { text: briefText });
      return data as CampaignContext;
    },
    onSuccess: (extracted) => {
      setContext((prev) => ({ ...prev, ...extracted }));
      setTalkingPointsText(joinLines(extracted.talking_points ?? []));
      setObjectionText(joinObjections(extracted.objection_handling ?? []));
      setQualifyingText(joinLines(extracted.qualifying_questions ?? []));
      setDoNotSayText(joinLines(extracted.do_not_say ?? []));
      toast.success('Context extracted and prefilled');
    },
    onError: () => toast.error('AI extract failed'),
  });

  const buildContextFromForm = (): CampaignContext => ({
    ...context,
    talking_points: splitLines(talkingPointsText),
    objection_handling: parseObjections(objectionText),
    qualifying_questions: splitLines(qualifyingText),
    do_not_say: splitLines(doNotSayText),
  });

  const createMutation = useMutation({
    mutationFn: async () => {
      const { data } = await api.post('/v1/campaigns', {
        ...draft,
        tenant_id: DEMO_TENANT_ID,
        status: 'draft',
        context: buildContextFromForm(),
      });
      return data as Campaign;
    },
    onSuccess: async (campaign) => {
      toast.success('Campaign created');
      setSelectedCampaignID(campaign.id);
      // Save limits immediately after creation
      try {
        await api.put(`/v1/campaigns/${campaign.id}/limits`, limits);
        toast.success('Limits saved');
      } catch {
        toast.error('Limits save failed — use Save Limits button');
      }
      queryClient.invalidateQueries({ queryKey: ['campaigns'] });
    },
    onError: () => toast.error('Campaign create failed'),
  });

  const limitsMutation = useMutation({
    mutationFn: async () => {
      if (!selectedCampaign) throw new Error('select campaign');
      await api.put(`/v1/campaigns/${selectedCampaign.id}/limits`, limits);
    },
    onSuccess: () => toast.success('Limits saved'),
    onError: () => toast.error('Save limits failed'),
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
        {/* ── Create Campaign ── */}
        <section className="space-y-4 rounded-lg border bg-card p-4">
          <h2 className="text-base font-semibold">Create Campaign</h2>

          {/* AI Brief */}
          <div className="rounded-md border bg-muted/30 p-3 space-y-2">
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
              AI Assist — paste a brief
            </p>
            <label className="grid gap-1 text-sm font-medium">
              Raw brief
              <textarea
                value={briefText}
                onChange={(e) => setBriefText(e.target.value)}
                rows={4}
                placeholder="Paste your product brief, script notes, or any text describing the campaign…"
                className="min-h-24 rounded-md border bg-background px-3 py-2 text-xs outline-none focus:ring-2 focus:ring-ring"
              />
            </label>
            <button
              type="button"
              onClick={() => extractMutation.mutate()}
              disabled={extractMutation.isPending || !briefText.trim()}
              className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50"
            >
              <Sparkles className="h-4 w-4" />
              {extractMutation.isPending ? 'Extracting…' : 'Extract & Prefill'}
            </button>
          </div>

          {/* Basic fields */}
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

          {/* AI Context fields */}
          <details className="group">
            <summary className="cursor-pointer select-none text-sm font-semibold text-muted-foreground hover:text-foreground">
              AI Context fields {extractMutation.isSuccess ? '(prefilled)' : ''}
            </summary>
            <div className="mt-3 grid gap-3">
              <label className="grid gap-1 text-sm">
                <span className="font-medium text-foreground">
                  Voice <span className="text-muted-foreground">(agent voice — sets the conversation gender)</span>
                </span>
                <select
                  className="rounded-md border border-input bg-background px-3 py-2 text-sm"
                  value={context.voice}
                  onChange={(e) => setContext((c) => ({ ...c, voice: e.target.value }))}
                >
                  {VOICE_OPTIONS.map((v) => (
                    <option key={v.id} value={v.id}>
                      {v.label} ({v.gender})
                    </option>
                  ))}
                </select>
              </label>
              <TextAreaField
                label="Product description"
                value={context.product_description}
                onChange={(v) => setContext((c) => ({ ...c, product_description: v }))}
                placeholder="What is the product or service?"
              />
              <TextAreaField
                label="Offer"
                value={context.offer}
                onChange={(v) => setContext((c) => ({ ...c, offer: v }))}
                placeholder="What is the specific offer being made?"
              />
              <TextAreaField
                label="Goal"
                value={context.goal}
                onChange={(v) => setContext((c) => ({ ...c, goal: v }))}
                placeholder="What should the agent achieve in this call?"
              />
              <Field
                label="Persona"
                value={context.persona}
                onChange={(v) => setContext((c) => ({ ...c, persona: v }))}
              />
              <Field
                label="Language"
                value={context.language}
                onChange={(v) => setContext((c) => ({ ...c, language: v }))}
              />
              <Field
                label="Business hours"
                value={context.business_hours}
                onChange={(v) => setContext((c) => ({ ...c, business_hours: v }))}
              />
              <TextAreaField
                label="Talking points (one per line or comma-separated)"
                value={talkingPointsText}
                onChange={setTalkingPointsText}
                placeholder="Free trial available&#10;No setup fees&#10;24/7 support"
              />
              <TextAreaField
                label="Qualifying questions (one per line or comma-separated)"
                value={qualifyingText}
                onChange={setQualifyingText}
                placeholder="Are you the decision maker?&#10;Current monthly spend?"
              />
              <TextAreaField
                label="Do not say (one per line or comma-separated)"
                value={doNotSayText}
                onChange={setDoNotSayText}
                placeholder="competitor name&#10;guaranteed"
              />
              <TextAreaField
                label='Objections ("objection :: response" — one per line)'
                value={objectionText}
                onChange={setObjectionText}
                placeholder="Too expensive :: We offer flexible pricing plans&#10;Not interested :: May I ask what your current solution is?"
                rows={4}
              />
            </div>
          </details>

          {/* Pacing & Limits */}
          <details className="group">
            <summary className="cursor-pointer select-none text-sm font-semibold text-muted-foreground hover:text-foreground">
              Pacing &amp; Limits
            </summary>
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <NumField
                label="Concurrent cap"
                value={limits.concurrent_cap}
                onChange={(v) => setLimits((l) => ({ ...l, concurrent_cap: v }))}
              />
              <NumField
                label="Hourly cap"
                value={limits.hourly_call_cap}
                onChange={(v) => setLimits((l) => ({ ...l, hourly_call_cap: v }))}
              />
              <NumField
                label="Daily cap"
                value={limits.daily_call_cap}
                onChange={(v) => setLimits((l) => ({ ...l, daily_call_cap: v }))}
              />
              <NumField
                label="Retry max"
                value={limits.retry_max}
                onChange={(v) => setLimits((l) => ({ ...l, retry_max: v }))}
              />
              <NumField
                label="Retry busy (min)"
                value={limits.retry_busy_min}
                onChange={(v) => setLimits((l) => ({ ...l, retry_busy_min: v }))}
              />
              <NumField
                label="Retry no-ans (min)"
                value={limits.retry_no_answer_min}
                onChange={(v) => setLimits((l) => ({ ...l, retry_no_answer_min: v }))}
              />
              <NumField
                label="Cost cap (₹)"
                value={limits.cost_cap_inr}
                onChange={(v) => setLimits((l) => ({ ...l, cost_cap_inr: v }))}
              />
              <NumField
                label="Max call (sec)"
                value={limits.max_call_seconds}
                onChange={(v) => setLimits((l) => ({ ...l, max_call_seconds: v }))}
              />
              <NumField
                label="Window start hr"
                value={limits.call_window_start_hour}
                onChange={(v) => setLimits((l) => ({ ...l, call_window_start_hour: v }))}
              />
              <NumField
                label="Window end hr"
                value={limits.call_window_end_hour}
                onChange={(v) => setLimits((l) => ({ ...l, call_window_end_hour: v }))}
              />
              <div className="sm:col-span-2">
                <Field
                  label="Timezone"
                  value={limits.timezone}
                  onChange={(v) => setLimits((l) => ({ ...l, timezone: v }))}
                />
              </div>
            </div>
          </details>

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
              Run Campaign
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
            <button
              type="button"
              onClick={() => limitsMutation.mutate()}
              disabled={!selectedCampaign || limitsMutation.isPending}
              className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-sm font-medium hover:bg-muted disabled:opacity-50"
            >
              {limitsMutation.isPending ? 'Saving…' : 'Save Limits'}
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

      {/* ── Live Progress Panel (only shown when active campaign selected) ── */}
      {isActive && (
        <section className="rounded-lg border bg-card p-4">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <h2 className="text-base font-semibold">Live Progress</h2>
            <span className="text-xs text-muted-foreground">
              {selectedCampaign.name} — auto-refreshes every 2 s
            </span>
          </div>
          {progressQuery.isFetching && !progressQuery.data ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : progressQuery.data ? (
            <dl className="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
              <Metric label="Placed" value={String(progressQuery.data.placed)} />
              <Metric label="Connected" value={String(progressQuery.data.connected)} />
              <Metric label="In-flight" value={String(progressQuery.data.in_flight)} />
              <Metric label="No-ans retry" value={String(progressQuery.data.no_answer_retry)} />
              <Metric label="Failed" value={String(progressQuery.data.failed)} />
              <Metric label="Suppressed" value={String(progressQuery.data.suppressed)} />
              <Metric
                label="Remaining"
                value={String(
                  Math.max(
                    0,
                    progressQuery.data.total -
                      progressQuery.data.done -
                      progressQuery.data.failed -
                      progressQuery.data.suppressed,
                  ),
                )}
              />
              <Metric label="Total" value={String(progressQuery.data.total)} />
            </dl>
          ) : (
            <p className="text-sm text-muted-foreground">No progress data yet.</p>
          )}
        </section>
      )}
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

function TextAreaField({
  label,
  value,
  onChange,
  placeholder,
  rows = 3,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  rows?: number;
}) {
  return (
    <label className="grid gap-1 text-sm font-medium">
      {label}
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        rows={rows}
        className="rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
      />
    </label>
  );
}

function NumField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <label className="grid gap-1 text-sm font-medium">
      {label}
      <input
        type="number"
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
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
