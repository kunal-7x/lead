import type { Metadata } from "next";
import { AdminLoginForm } from "@/components/auth/admin-login-form";

export const metadata: Metadata = { title: "Login — EVS Internal Admin" };

export default function LoginPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-4">
      <div className="w-full max-w-sm space-y-6">
        <div className="space-y-2 text-center">
          <h1 className="text-3xl font-bold">EVS Admin</h1>
          <p className="text-muted-foreground text-sm">
            Internal access only
          </p>
        </div>
        <AdminLoginForm />
      </div>
    </main>
  );
}
