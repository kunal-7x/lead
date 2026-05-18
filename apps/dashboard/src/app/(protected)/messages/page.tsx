'use client';

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { MessageCircle, Paperclip, Search, Send } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/lib/api';

const DEMO_TENANT_ID = 'tenant-demo';

type Direction = 'inbound' | 'outbound';

interface RawThread {
  id?: string;
  ID?: string;
  lead_id?: string;
  LeadID?: string;
  phone?: string;
  Phone?: string;
  updated_at?: string;
  UpdatedAt?: string;
  service_window_until?: string;
  ServiceWindowUntil?: string;
}

interface RawMessage {
  id?: string;
  ID?: string;
  thread_id?: string;
  ThreadID?: string;
  direction?: Direction;
  Direction?: Direction;
  body?: string;
  Body?: string;
  status?: string;
  Status?: string;
  created_at?: string;
  CreatedAt?: string;
}

interface InboxThread {
  id: string;
  leadID: string;
  lead: string;
  phone: string;
  project: string;
  status: string;
  updatedAt: string;
}

interface InboxMessage {
  id: string;
  from: 'lead' | 'agent';
  body: string;
  status: string;
  createdAt: string;
}

const LEAD_LABELS: Record<string, { name: string; project: string }> = {
  'lead-demo-001': { name: 'Asha Mehta', project: 'Skyline Residency' },
  'lead-demo-002': { name: 'Rohan Shah', project: 'Lakeview Towers' },
  'lead-demo-003': { name: 'Neha Iyer', project: 'Green Acres' },
};

async function fetchThreads() {
  const { data } = await api.get(`/v1/whatsapp/threads?tenant_id=${DEMO_TENANT_ID}`);
  return ((data.threads ?? []) as RawThread[]).map(normalizeThread);
}

async function fetchMessages(threadID: string) {
  const { data } = await api.get(`/v1/whatsapp/threads/${threadID}/messages`);
  return ((data.messages ?? []) as RawMessage[]).map(normalizeMessage);
}

function normalizeThread(raw: RawThread): InboxThread {
  const id = raw.id ?? raw.ID ?? '';
  const leadID = raw.lead_id ?? raw.LeadID ?? '';
  const phone = raw.phone ?? raw.Phone ?? '';
  const labels = LEAD_LABELS[leadID] ?? { name: leadID || phone, project: 'Demo project' };
  const serviceWindow = raw.service_window_until ?? raw.ServiceWindowUntil ?? '';
  return {
    id,
    leadID,
    lead: labels.name,
    phone,
    project: labels.project,
    status: serviceWindow ? 'Open' : 'Template sent',
    updatedAt: raw.updated_at ?? raw.UpdatedAt ?? '',
  };
}

function normalizeMessage(raw: RawMessage): InboxMessage {
  const direction = raw.direction ?? raw.Direction ?? 'outbound';
  return {
    id: raw.id ?? raw.ID ?? '',
    from: direction === 'inbound' ? 'lead' : 'agent',
    body: raw.body ?? raw.Body ?? '',
    status: raw.status ?? raw.Status ?? '',
    createdAt: raw.created_at ?? raw.CreatedAt ?? '',
  };
}

export default function MessagesPage() {
  const queryClient = useQueryClient();
  const [selectedThreadID, setSelectedThreadID] = useState('');
  const [query, setQuery] = useState('');
  const [reply, setReply] = useState('');

  const { data: threads = [], isLoading } = useQuery({
    queryKey: ['whatsapp-threads'],
    queryFn: fetchThreads,
  });

  useEffect(() => {
    if (!selectedThreadID && threads[0]) {
      setSelectedThreadID(threads[0].id);
    }
  }, [selectedThreadID, threads]);

  const selectedThread = threads.find((thread) => thread.id === selectedThreadID) ??
    threads[0] ?? {
      id: '',
      leadID: '',
      lead: '',
      phone: '',
      project: '',
      status: '',
      updatedAt: '',
    };

  const { data: messages = [] } = useQuery({
    queryKey: ['whatsapp-messages', selectedThread.id],
    queryFn: () => fetchMessages(selectedThread.id),
    enabled: Boolean(selectedThread.id),
  });

  const filteredThreads = useMemo(
    () =>
      threads.filter((thread) =>
        `${thread.lead} ${thread.phone} ${thread.project}`
          .toLowerCase()
          .includes(query.toLowerCase()),
      ),
    [query, threads],
  );

  const latestMessageByThread = useMemo(() => {
    if (!selectedThread.id || messages.length === 0) return new Map<string, string>();
    return new Map([[selectedThread.id, messages[messages.length - 1]?.body ?? '']]);
  }, [messages, selectedThread.id]);

  const sendMutation = useMutation({
    mutationFn: async () => {
      const body = reply.trim();
      if (!body || !selectedThread.id) return;
      await api.post('/v1/whatsapp/messages', {
        tenant_id: DEMO_TENANT_ID,
        thread_id: selectedThread.id,
        body,
      });
    },
    onSuccess: () => {
      setReply('');
      queryClient.invalidateQueries({ queryKey: ['whatsapp-messages', selectedThread.id] });
      toast.success('Message queued');
    },
    onError: () => toast.error('Message failed'),
  });

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">WhatsApp Inbox</h1>
          <p className="text-sm text-muted-foreground">
            Tenant conversations and service-window replies.
          </p>
        </div>
        <Link
          href="/messages/templates"
          className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          <MessageCircle className="h-4 w-4" />
          Templates
        </Link>
      </div>

      <div className="grid min-h-[620px] gap-4 lg:grid-cols-[320px_1fr]">
        <section className="overflow-hidden rounded-lg border bg-card">
          <div className="border-b p-3">
            <label className="relative block">
              <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted-foreground" />
              <input
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search lead or phone"
                className="h-9 w-full rounded-md border bg-background pl-9 pr-3 text-sm outline-none focus:ring-2 focus:ring-ring"
              />
            </label>
          </div>
          <div className="divide-y">
            {isLoading ? (
              <div className="p-4 text-sm text-muted-foreground">Loading conversations...</div>
            ) : filteredThreads.length === 0 ? (
              <div className="p-4 text-sm text-muted-foreground">No conversations found.</div>
            ) : (
              filteredThreads.map((thread) => (
                <button
                  key={thread.id}
                  type="button"
                  onClick={() => setSelectedThreadID(thread.id)}
                  className={`grid w-full gap-1 px-4 py-3 text-left hover:bg-muted/40 ${
                    selectedThreadID === thread.id ? 'bg-muted/60' : ''
                  }`}
                >
                  <span className="flex items-center justify-between gap-3">
                    <span className="truncate text-sm font-medium">{thread.lead}</span>
                    <span className="rounded-full border px-2 py-0.5 text-xs">{thread.status}</span>
                  </span>
                  <span className="truncate text-xs text-muted-foreground">{thread.project}</span>
                  <span className="truncate text-xs">
                    {latestMessageByThread.get(thread.id) || thread.phone}
                  </span>
                </button>
              ))
            )}
          </div>
        </section>

        <section className="flex overflow-hidden rounded-lg border bg-card">
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex items-center justify-between gap-3 border-b px-5 py-4">
              <div className="min-w-0">
                <h2 className="truncate text-lg font-semibold">{selectedThread.lead}</h2>
                <p className="truncate text-sm text-muted-foreground">
                  {selectedThread.phone} - {selectedThread.project}
                </p>
              </div>
              <span className="rounded-full border px-3 py-1 text-xs font-medium">
                {selectedThread.status}
              </span>
            </div>

            <div className="flex-1 space-y-3 overflow-y-auto bg-muted/20 p-5">
              {messages.length === 0 ? (
                <p className="text-sm text-muted-foreground">No messages yet.</p>
              ) : (
                messages.map((message) => (
                  <div
                    key={message.id}
                    className={`flex ${message.from === 'agent' ? 'justify-end' : 'justify-start'}`}
                  >
                    <div
                      className={`max-w-[78%] rounded-lg px-4 py-2 text-sm shadow-sm ${
                        message.from === 'agent'
                          ? 'bg-primary text-primary-foreground'
                          : 'border bg-background'
                      }`}
                    >
                      <p>{message.body}</p>
                      <p
                        className={`mt-1 text-[11px] ${
                          message.from === 'agent'
                            ? 'text-primary-foreground/70'
                            : 'text-muted-foreground'
                        }`}
                      >
                        {formatTime(message.createdAt)} - {message.status}
                      </p>
                    </div>
                  </div>
                ))
              )}
            </div>

            <div className="border-t p-4">
              <div className="flex items-center gap-2">
                <button
                  className="inline-flex h-10 w-10 items-center justify-center rounded-md border hover:bg-muted"
                  aria-label="Attach file"
                >
                  <Paperclip className="h-4 w-4" />
                </button>
                <input
                  aria-label="Quick reply"
                  value={reply}
                  onChange={(event) => setReply(event.target.value)}
                  placeholder="Reply inside the 24h window"
                  className="h-10 min-w-0 flex-1 rounded-md border px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
                <button
                  onClick={() => sendMutation.mutate()}
                  disabled={!reply.trim() || sendMutation.isPending}
                  className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                >
                  <Send className="h-4 w-4" />
                  Send
                </button>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}

function formatTime(value: string) {
  if (!value) return 'now';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
