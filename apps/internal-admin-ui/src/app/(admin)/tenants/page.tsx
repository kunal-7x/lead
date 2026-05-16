import type { Metadata } from "next";
export const metadata: Metadata = { title: "Tenants — EVS Admin" };
export default function TenantsPage() {
  return <div className="space-y-4"><h1 className="text-2xl font-semibold">Tenants</h1><p className="text-muted-foreground text-sm">View and manage all tenants.</p></div>;
}
