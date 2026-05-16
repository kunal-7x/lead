import type { Metadata } from "next";
export const metadata: Metadata = { title: "Providers — EVS Admin" };
export default function ProvidersPage() {
  return <div className="space-y-4"><h1 className="text-2xl font-semibold">Providers</h1><p className="text-muted-foreground text-sm">Configure telephony and messaging providers.</p></div>;
}
