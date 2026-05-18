'use client';

import { useState } from 'react';
import Link from 'next/link';
import { CheckCircle2, MessageCircle, Plus, RefreshCw } from 'lucide-react';

const existingTemplates = [
  { name: 'lead-warm-up', category: 'marketing', status: 'Approved', language: 'en' },
  { name: 'site-visit-reminder', category: 'utility', status: 'Approved', language: 'en' },
  { name: 'post-call-summary', category: 'utility', status: 'Draft', language: 'en' },
  { name: 'handover-intro', category: 'utility', status: 'Draft', language: 'en' },
];

export default function TemplatesPage() {
  const [category, setCategory] = useState('utility');

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">WhatsApp Templates</h1>
          <p className="text-sm text-muted-foreground">
            Builder, category gate, and Meta sync status.
          </p>
        </div>
        <div className="flex gap-2">
          <Link
            href="/messages"
            className="inline-flex items-center gap-2 rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted"
          >
            <MessageCircle className="h-4 w-4" />
            Inbox
          </Link>
          <button className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90">
            <RefreshCw className="h-4 w-4" />
            Sync Meta
          </button>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[1fr_360px]">
        <section className="rounded-lg border bg-card p-5">
          <div className="mb-5 flex items-center justify-between gap-3">
            <h2 className="text-lg font-semibold">Template Builder</h2>
            <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-3 py-1 text-xs font-medium text-emerald-700">
              <CheckCircle2 className="h-3.5 w-3.5" />
              Category enforced
            </span>
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <label className="grid gap-1.5 text-sm font-medium">
              Template name
              <input
                className="h-10 rounded-md border px-3 text-sm font-normal outline-none focus:ring-2 focus:ring-ring"
                defaultValue="site-visit-reminder"
              />
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              Language
              <select
                className="h-10 rounded-md border bg-background px-3 text-sm font-normal outline-none focus:ring-2 focus:ring-ring"
                defaultValue="en"
              >
                <option value="en">English</option>
                <option value="hi">Hindi</option>
                <option value="mr">Marathi</option>
              </select>
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              Category
              <select
                value={category}
                onChange={(event) => setCategory(event.target.value)}
                className="h-10 rounded-md border bg-background px-3 text-sm font-normal outline-none focus:ring-2 focus:ring-ring"
              >
                <option value="marketing">Marketing</option>
                <option value="utility">Utility</option>
                <option value="authentication">Authentication</option>
              </select>
            </label>
            <label className="grid gap-1.5 text-sm font-medium">
              Meta status
              <input
                className="h-10 rounded-md border px-3 text-sm font-normal outline-none focus:ring-2 focus:ring-ring"
                defaultValue="Draft"
              />
            </label>
          </div>

          <label className="mt-4 grid gap-1.5 text-sm font-medium">
            Body
            <textarea
              className="min-h-36 rounded-md border p-3 text-sm font-normal outline-none focus:ring-2 focus:ring-ring"
              defaultValue="Reminder: your site visit for {{project_name}} is scheduled at {{visit_time}}."
            />
          </label>

          <div className="mt-5 flex flex-wrap gap-2">
            <button className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90">
              <Plus className="h-4 w-4" />
              Register
            </button>
            <button className="rounded-md border px-4 py-2 text-sm font-medium hover:bg-muted">
              Submit to Meta
            </button>
          </div>
        </section>

        <section className="rounded-lg border bg-card">
          <div className="border-b px-4 py-3">
            <h2 className="text-sm font-semibold">Pre-built Templates</h2>
          </div>
          <div className="divide-y">
            {existingTemplates.map((template) => (
              <div key={template.name} className="px-4 py-3">
                <div className="flex items-center justify-between gap-3">
                  <p className="truncate text-sm font-medium">{template.name}</p>
                  <span className="rounded-full border px-2 py-0.5 text-xs">{template.status}</span>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  {template.category} - {template.language}
                </p>
              </div>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}
