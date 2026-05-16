import { LoginForm } from "@/components/auth/login-form";
import type { Metadata } from "next";

export const metadata: Metadata = { title: "Login — EVS" };

export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-2 text-center">
          <h1 className="text-3xl font-bold">EVS</h1>
          <p className="text-muted-foreground text-sm">
            Sign in to your workspace
          </p>
        </div>
        <LoginForm />
      </div>
    </main>
  );
}
