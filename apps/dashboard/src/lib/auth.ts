"use client";

import { api, setTokens, clearTokens } from "./api";

export interface LoginPayload {
  email: string;
  password: string;
  tenant_id: string;
}

export interface LoginResponse {
  access_token: string;
  refresh_token?: string;
  expires_in?: number;
  requires_2fa?: boolean;
}

export async function login(payload: LoginPayload): Promise<LoginResponse> {
  const { data } = await api.post<LoginResponse>("/v1/auth/login", payload);
  // The backend is Bearer-token based and sets no cookie, so the client must
  // persist the JWT and attach it to subsequent requests (see api.ts).
  if (data.access_token && !data.requires_2fa) {
    setTokens(data.access_token, data.refresh_token);
  }
  return data;
}

export async function verify2FA(code: string, tenant_id: string) {
  const { data } = await api.post<LoginResponse>("/v1/auth/2fa/verify", {
    code,
    tenant_id,
  });
  if (data?.access_token) {
    setTokens(data.access_token, data.refresh_token);
  }
  return data;
}

export async function logout() {
  try {
    await api.post("/v1/auth/logout");
  } finally {
    clearTokens();
  }
}

export async function refresh() {
  const { data } = await api.post("/v1/auth/refresh");
  return data;
}
