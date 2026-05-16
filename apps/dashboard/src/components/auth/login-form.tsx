"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { login, verify2FA } from "@/lib/auth";

export function LoginForm() {
  const router = useRouter();
  const [step, setStep] = useState<"credentials" | "2fa">("credentials");
  const [loading, setLoading] = useState(false);
  const [tenantId, setTenantId] = useState("");
  const [form, setForm] = useState({ email: "", password: "", tenant: "" });
  const [code, setCode] = useState("");

  async function handleCredentials(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    try {
      const res = await login({
        email: form.email,
        password: form.password,
        tenant_id: form.tenant,
      });
      setTenantId(form.tenant);
      if (res.requires_2fa) {
        setStep("2fa");
      } else {
        router.push("/dashboard");
      }
    } catch (err: unknown) {
      const msg =
        err instanceof Error ? err.message : "Login failed. Please try again.";
      toast.error(msg);
    } finally {
      setLoading(false);
    }
  }

  async function handle2FA(e: React.FormEvent) {
    e.preventDefault();
    setLoading(true);
    try {
      await verify2FA(code, tenantId);
      router.push("/dashboard");
    } catch {
      toast.error("Invalid 2FA code.");
    } finally {
      setLoading(false);
    }
  }

  if (step === "2fa") {
    return (
      <form onSubmit={handle2FA} className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="code">Authenticator code</Label>
          <Input
            id="code"
            inputMode="numeric"
            pattern="[0-9]{6}"
            maxLength={6}
            placeholder="000000"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            required
            autoFocus
          />
        </div>
        <Button type="submit" className="w-full" disabled={loading}>
          {loading ? "Verifying…" : "Verify"}
        </Button>
        <button
          type="button"
          className="text-muted-foreground w-full text-center text-sm underline"
          onClick={() => setStep("credentials")}
        >
          Back to login
        </button>
      </form>
    );
  }

  return (
    <form onSubmit={handleCredentials} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="tenant">Workspace ID</Label>
        <Input
          id="tenant"
          placeholder="your-workspace"
          value={form.tenant}
          onChange={(e) => setForm((f) => ({ ...f, tenant: e.target.value }))}
          required
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="email">Email</Label>
        <Input
          id="email"
          type="email"
          placeholder="you@example.com"
          value={form.email}
          onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
          required
          autoComplete="email"
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="password">Password</Label>
        <Input
          id="password"
          type="password"
          placeholder="••••••••"
          value={form.password}
          onChange={(e) => setForm((f) => ({ ...f, password: e.target.value }))}
          required
          autoComplete="current-password"
        />
      </div>
      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? "Signing in…" : "Sign in"}
      </Button>
      <p className="text-muted-foreground text-center text-sm">
        <a href="/reset-password" className="underline">
          Forgot password?
        </a>
      </p>
    </form>
  );
}
