'use client';

import { useMemo, useState } from 'react';
import Link from 'next/link';
import { MessageCircle, Paperclip, Search, Send } from 'lucide-react';

const threads = [
  {
    id: 'th-1',
    lead: 'Asha Mehta',
    phone: '+91 98765 43210',
    project: 'Skyline Residency',
    last: 'STOP',
    unread: 1,
    status: 'Opted out',
  },
  {
    id: 'th-2',
    lead: 'Rohan Shah',
    phone: '+91 99887 76655',
    project: 'Lakeview Towers',
    last: 'Can I visit on Saturday?',
    unread: 2,
    status: 'Open',
  },
  {
    id: 'th-3',
    lead: 'Neha Iyer',
    phone: '+91 91234 56780',
    project: 'Green Acres',
    last: 'Site visit reminder sent',
    unread: 0,
    status: 'Template sent',
  },
];

const messagesByThread: Record<
  string,
  { id: string; from: 'lead' | 'agent'; body: string; time: string }[]
> = {
  'th-1': [
    {
      id: 'm1',
      from: 'agent',
      body: 'Hi Asha, thanks for your interest in Skyline Residency.',
      time: '09:42',
    },
    { id: 'm2', from: 'lead', body: 'STOP', time: '09:48' },
  ],
  'th-2': [
    { id: 'm3', from: 'lead', body: 'Can I visit on Saturday?', time: '11:12' },
    { id: 'm4', from: 'agent', body: 'Yes. I can hold a 4 PM slot for you.', time: '11:14' },
  ],
  'th-3': [
    {
      id: 'm5',
      from: 'agent',
      body: 'Reminder: your site visit is scheduled tomorrow at 10 AM.',
      time: 'Yesterday',
    },
  ],
};

export default function MessagesPage() {
  const [selectedThreadID, setSelectedThreadID] = useState('th-2');
  const [query, setQuery] = useState('');
  const selectedThread = threads.find((thread) => thread.id === selectedThreadID) ??
    threads[0] ?? {
      id: '',
      lead: '',
      phone: '',
      project: '',
      last: '',
      unread: 0,
      status: '',
    };
  const filteredThreads = useMemo(
    () =>
      threads.filter((thread) =>
        `${thread.lead} ${thread.phone} ${thread.project}`
          .toLowerCase()
          .includes(query.toLowerCase()),
      ),
    [query],
  );
  const messages = messagesByThread[selectedThread.id] ?? [];

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
            {filteredThreads.map((thread) => (
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
                  {thread.unread > 0 ? (
                    <span className="rounded-full bg-primary px-2 py-0.5 text-xs text-primary-foreground">
                      {thread.unread}
                    </span>
                  ) : null}
                </span>
                <span className="truncate text-xs text-muted-foreground">{thread.project}</span>
                <span className="truncate text-xs">{thread.last}</span>
              </button>
            ))}
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
              {messages.map((message) => (
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
                      className={`mt-1 text-[11px] ${message.from === 'agent' ? 'text-primary-foreground/70' : 'text-muted-foreground'}`}
                    >
                      {message.time}
                    </p>
                  </div>
                </div>
              ))}
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
                  placeholder="Reply inside the 24h window"
                  className="h-10 min-w-0 flex-1 rounded-md border px-3 text-sm outline-none focus:ring-2 focus:ring-ring"
                />
                <button className="inline-flex h-10 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90">
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
