import type { Metadata } from "next";
export const metadata: Metadata = { title: "Audit — EVS Admin" };
export default function AuditPage() {
  return <div className="space-y-4"><h1 className="text-2xl font-semibold">Audit</h1><p className="text-muted-foreground text-sm">System-wide audit trail.</p></div>;
}
