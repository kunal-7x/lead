import type { Metadata } from "next";
export const metadata: Metadata = { title: "Support — EVS Admin" };
export default function SupportPage() {
  return <div className="space-y-4"><h1 className="text-2xl font-semibold">Support</h1><p className="text-muted-foreground text-sm">View and manage customer support tickets.</p></div>;
}
