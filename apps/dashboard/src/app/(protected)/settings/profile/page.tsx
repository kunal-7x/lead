import type { Metadata } from "next";

export const metadata: Metadata = { title: "Profile — EVS" };

export default function ProfilePage() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Profile</h1>
      <p className="text-muted-foreground text-sm">
        Manage your personal account settings.
      </p>
    </div>
  );
}
