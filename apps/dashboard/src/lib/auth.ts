"use client";

import { api } from "./api";

export interface LoginPayload {
  email: string;
  password: string;
  tenant_id: string;
}

export interface LoginResponse {
  access_token: string;
  requires_2fa?: boolean;
}

export async function login(payload: LoginPayload): Promise<LoginResponse> {
  const { data } = await api.post<LoginResponse>("/v1/auth/login", payload);
  return data;
}

export async function verify2FA(code: string, tenant_id: string) {
  const { data } = await api.post("/v1/auth/2fa/verify", { code, tenant_id });
  return data;
}

export async function logout() {
  await api.post("/v1/auth/logout");
}

export async function refresh() {
  const { data } = await api.post("/v1/auth/refresh");
  return data;
}
