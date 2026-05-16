import type { Metadata } from "next";
export const metadata: Metadata = { title: "Jobs — EVS Admin" };
export default function JobsPage() {
  return <div className="space-y-4"><h1 className="text-2xl font-semibold">Jobs</h1><p className="text-muted-foreground text-sm">Monitor background jobs and Temporal workflows.</p></div>;
}
