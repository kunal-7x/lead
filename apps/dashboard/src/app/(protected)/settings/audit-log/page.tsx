import type { Metadata } from "next";

export const metadata: Metadata = { title: "Audit Log — EVS" };

export default function AuditLogPage() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Audit Log</h1>
      <p className="text-muted-foreground text-sm">
        Review all account activity and changes.
      </p>
    </div>
  );
}
