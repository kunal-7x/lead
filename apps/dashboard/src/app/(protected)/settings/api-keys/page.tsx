import type { Metadata } from "next";

export const metadata: Metadata = { title: "API Keys — EVS" };

export default function ApiKeysPage() {
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">API Keys</h1>
      <p className="text-muted-foreground text-sm">
        Create and manage API keys for programmatic access.
      </p>
    </div>
  );
}
