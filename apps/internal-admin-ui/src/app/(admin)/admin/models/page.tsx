import type { Metadata } from 'next';
import { AlertTriangle, CheckCircle2, Save, Trash2, XCircle } from 'lucide-react';
import { globalModelSelection, modelOptions, tenantModelOverrides } from '@/lib/admin-data';

export const metadata: Metadata = { title: 'Model Switcher - Capsy Admin' };

export default function ModelSwitcherPage() {
  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-normal">Model Switcher</h1>
        <p className="max-w-3xl text-sm text-muted-foreground">
          Super admin controls for global LLM, STT, and TTS routing plus tenant-specific overrides.
        </p>
      </div>

      <section className="rounded-lg border bg-card p-4" aria-label="Global model routing">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-base font-semibold">Global Routing</h2>
          <button className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground">
            <Save className="h-4 w-4" aria-hidden="true" />
            Save
          </button>
        </div>
        <div className="grid gap-4 lg:grid-cols-3">
          <ModelSelect
            label="Global LLM"
            value={globalModelSelection.llm}
            options={modelOptions.llm}
          />
          <ModelSelect
            label="Global STT"
            value={globalModelSelection.stt}
            options={modelOptions.stt}
          />
          <ModelSelect
            label="Global TTS"
            value={globalModelSelection.tts}
            options={modelOptions.tts}
          />
        </div>
      </section>

      <section className="rounded-lg border bg-card p-4">
        <div className="mb-4 flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
          <h2 className="text-base font-semibold">Tenant Overrides</h2>
          <div className="flex flex-wrap gap-2">
            <input
              className="h-9 rounded-md border px-3 text-sm"
              placeholder="tenant-id"
              aria-label="Tenant id"
            />
            <button className="inline-flex h-9 items-center gap-2 rounded-md border px-3 text-sm">
              <Save className="h-4 w-4" aria-hidden="true" />
              Add override
            </button>
          </div>
        </div>
        <div className="overflow-x-auto">
          <table
            className="w-full min-w-[760px] text-left text-sm"
            aria-label="Tenant model overrides"
          >
            <thead className="border-b text-xs uppercase text-muted-foreground">
              <tr>
                <th className="py-2 pr-3 font-medium">Tenant</th>
                <th className="py-2 pr-3 font-medium">LLM</th>
                <th className="py-2 pr-3 font-medium">STT</th>
                <th className="py-2 pr-3 font-medium">TTS</th>
                <th className="py-2 pr-3 font-medium">Updated</th>
                <th className="py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tenantModelOverrides.map((override) => (
                <tr key={override.tenant} className="border-b last:border-b-0">
                  <td className="py-3 pr-3">
                    <div className="font-medium">{override.name}</div>
                    <div className="text-xs text-muted-foreground">{override.tenant}</div>
                  </td>
                  <td className="py-3 pr-3">{override.llm}</td>
                  <td className="py-3 pr-3">{override.stt}</td>
                  <td className="py-3 pr-3">{override.tts}</td>
                  <td className="py-3 pr-3">{override.updated}</td>
                  <td className="py-3">
                    <div className="flex gap-2">
                      <button className="rounded-md border px-2 py-1 text-xs">Edit</button>
                      <button className="inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs text-red-700">
                        <Trash2 className="h-3 w-3" aria-hidden="true" />
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="rounded-lg border bg-card p-4" aria-label="Engine health panel">
        <h2 className="mb-4 text-base font-semibold">Engine Health</h2>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {[...modelOptions.llm, ...modelOptions.stt, ...modelOptions.tts].map((engine) => (
            <div key={engine.id} className="rounded-md border p-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <div className="font-medium">{engine.name}</div>
                  <div className="text-xs text-muted-foreground">
                    {engine.type} - {engine.cost} - p95 {engine.latency}
                  </div>
                </div>
                {engine.healthy ? (
                  <CheckCircle2
                    className="h-5 w-5 text-emerald-600"
                    aria-label={`${engine.name} online`}
                  />
                ) : (
                  <XCircle className="h-5 w-5 text-red-600" aria-label={`${engine.name} offline`} />
                )}
              </div>
              {engine.requiresGpu && !engine.healthy ? (
                <div className="mt-3 inline-flex items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-2 py-1 text-xs text-amber-800">
                  <AlertTriangle className="h-3 w-3" aria-hidden="true" />
                  GPU endpoint missing
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function ModelSelect({
  label,
  value,
  options,
}: {
  label: string;
  value: string;
  options: Array<{
    id: string;
    name: string;
    type: string;
    cost: string;
    latency: string;
    requiresGpu: boolean;
    healthy: boolean;
  }>;
}) {
  return (
    <label className="space-y-2">
      <span className="text-sm font-medium">{label}</span>
      <select
        className="h-10 w-full rounded-md border bg-background px-3 text-sm"
        defaultValue={value}
        aria-label={label}
      >
        {options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.name} - {option.cost} - {option.latency}
          </option>
        ))}
      </select>
      <span className="block text-xs text-muted-foreground">
        Writes the Redis key used by the live router.
      </span>
    </label>
  );
}
