"use client";

import { useState, useRef } from "react";
import { useMutation } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { api } from "@/lib/api";

type Step = "upload" | "mapping" | "preview" | "done";

interface PreviewRow {
  [col: string]: string;
}

interface RowError {
  row: number;
  message: string;
}

const CANONICAL_FIELDS = ["phone", "email", "name", "source_id", "assigned_to"];

export default function ImportPage() {
  const router = useRouter();
  const fileRef = useRef<HTMLInputElement>(null);

  const [step, setStep] = useState<Step>("upload");
  const [file, setFile] = useState<File | null>(null);
  const [previewRows, setPreviewRows] = useState<PreviewRow[]>([]);
  const [previewErrors, setPreviewErrors] = useState<RowError[]>([]);
  const [columns, setColumns] = useState<string[]>([]);
  const [mapping, setMapping] = useState<Record<string, string>>({});

  const previewMutation = useMutation({
    mutationFn: async (f: File) => {
      const fd = new FormData();
      fd.append("file", f);
      fd.append("mapping", JSON.stringify(mapping));
      const { data } = await api.post("/v1/import/preview", fd);
      return data as { rows: PreviewRow[]; errors: RowError[] };
    },
    onSuccess: (data) => {
      setPreviewRows(data.rows ?? []);
      setPreviewErrors(data.errors ?? []);
      if (data.rows && data.rows.length > 0 && data.rows[0]) {
        setColumns(Object.keys(data.rows[0]));
      }
      setStep("mapping");
    },
    onError: () => toast.error("Preview failed"),
  });

  const importMutation = useMutation({
    mutationFn: async () => {
      const rows = previewRows.map((row) => {
        const mapped: Record<string, string> = {};
        for (const [col, val] of Object.entries(row)) {
          const canonical = mapping[col] || col;
          mapped[canonical] = val;
        }
        return mapped;
      });
      const { data } = await api.post("/v1/import/jobs", { rows, mapping });
      return data;
    },
    onSuccess: () => {
      toast.success("Import started");
      setStep("done");
    },
    onError: () => toast.error("Import failed"),
  });

  function handleFileChange(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    if (!f) return;
    setFile(f);
    setStep("upload");
  }

  function handlePreview() {
    if (!file) return;
    previewMutation.mutate(file);
  }

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Import Leads</h1>
        <button
          onClick={() => router.push("/leads")}
          className="text-sm text-muted-foreground hover:underline"
        >
          Back to leads
        </button>
      </div>

      {/* Step indicator */}
      <div className="flex gap-2">
        {(["upload", "mapping", "preview", "done"] as Step[]).map((s, i) => (
          <div key={s} className="flex items-center gap-2">
            <span
              className={`flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold ${
                step === s
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground"
              }`}
            >
              {i + 1}
            </span>
            <span className="text-sm capitalize text-muted-foreground">{s}</span>
            {i < 3 && <span className="text-muted-foreground">›</span>}
          </div>
        ))}
      </div>

      {step === "upload" && (
        <div className="rounded-lg border bg-card p-6 space-y-4">
          <p className="text-sm text-muted-foreground">
            Upload a CSV or XLSX file containing lead data. The file must have
            a header row.
          </p>
          <input
            ref={fileRef}
            type="file"
            accept=".csv,.xlsx"
            onChange={handleFileChange}
            className="block w-full text-sm file:mr-4 file:rounded-md file:border-0 file:bg-primary file:px-4 file:py-2 file:text-sm file:font-medium file:text-primary-foreground"
          />
          {file && (
            <div className="flex items-center justify-between rounded-md bg-muted px-4 py-2 text-sm">
              <span>{file.name}</span>
              <button
                onClick={handlePreview}
                disabled={previewMutation.isPending}
                className="rounded-md bg-primary px-4 py-1.5 text-primary-foreground text-sm font-medium disabled:opacity-50"
              >
                {previewMutation.isPending ? "Previewing…" : "Preview"}
              </button>
            </div>
          )}
        </div>
      )}

      {(step === "mapping" || step === "preview") && (
        <div className="rounded-lg border bg-card p-6 space-y-4">
          <h2 className="font-semibold">Map columns</h2>
          <p className="text-xs text-muted-foreground">
            Map each file column to a canonical field. Unmapped columns are
            stored as-is.
          </p>
          <div className="grid grid-cols-2 gap-3">
            {columns.map((col) => (
              <div key={col} className="flex items-center gap-2">
                <span className="min-w-28 text-sm font-mono">{col}</span>
                <span className="text-muted-foreground">→</span>
                <select
                  value={mapping[col] ?? ""}
                  onChange={(e) =>
                    setMapping((m) => ({ ...m, [col]: e.target.value }))
                  }
                  className="flex-1 rounded-md border px-2 py-1 text-sm"
                >
                  <option value="">(keep as {col})</option>
                  {CANONICAL_FIELDS.map((f) => (
                    <option key={f} value={f}>
                      {f}
                    </option>
                  ))}
                </select>
              </div>
            ))}
          </div>

          {/* Preview table */}
          {previewRows.length > 0 && (
            <div className="overflow-x-auto rounded-md border">
              <table className="w-full text-xs">
                <thead className="bg-muted/50">
                  <tr>
                    {columns.map((c) => (
                      <th key={c} className="px-3 py-2 text-left font-medium">
                        {c}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {previewRows.slice(0, 10).map((row, i) => (
                    <tr key={i}>
                      {columns.map((c) => (
                        <td key={c} className="px-3 py-1.5">
                          {row[c] ?? ""}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {previewErrors.length > 0 && (
            <div className="rounded-md border border-red-200 bg-red-50 p-3">
              <p className="mb-1 text-xs font-medium text-red-700">
                {previewErrors.length} validation error(s)
              </p>
              <ul className="space-y-0.5 text-xs text-red-600">
                {previewErrors.slice(0, 5).map((e) => (
                  <li key={e.row}>
                    Row {e.row}: {e.message}
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="flex justify-end gap-3">
            <button
              onClick={() => setStep("upload")}
              className="rounded-md border px-4 py-2 text-sm"
            >
              Back
            </button>
            <button
              onClick={() => importMutation.mutate()}
              disabled={importMutation.isPending}
              className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
            >
              {importMutation.isPending ? "Importing…" : `Import ${previewRows.length} rows`}
            </button>
          </div>
        </div>
      )}

      {step === "done" && (
        <div className="rounded-lg border bg-card p-8 text-center space-y-4">
          <div className="text-4xl">✓</div>
          <h2 className="text-lg font-semibold">Import complete</h2>
          <p className="text-sm text-muted-foreground">
            Your leads have been imported and are now being processed.
          </p>
          <button
            onClick={() => router.push("/leads")}
            className="rounded-md bg-primary px-6 py-2 text-sm font-medium text-primary-foreground"
          >
            View leads
          </button>
        </div>
      )}
    </div>
  );
}
