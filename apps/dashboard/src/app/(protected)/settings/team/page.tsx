import type { Metadata } from "next";

export const metadata: Metadata = { title: "Team — EVS" };

export default function TeamPage() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Team</h1>
      <p className="text-muted-foreground text-sm">
        Manage team members and their roles.
      </p>
    </div>
  );
}
