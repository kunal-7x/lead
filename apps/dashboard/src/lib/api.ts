import axios, { type AxiosRequestConfig } from "axios";

const ACCESS_KEY = "evs.access_token";
const REFRESH_KEY = "evs.refresh_token";

// The BFF authenticates via `Authorization: Bearer <jwt>` and sets NO cookie,
// so the client is responsible for persisting the token and attaching it to
// every request. We keep an in-memory copy (fast, survives same-tab navigation
// via the hydrate-from-storage below) backed by localStorage for reloads.
let accessToken: string | null = null;
let refreshToken: string | null = null;

function hydrate() {
  if (typeof window === "undefined") return;
  if (accessToken === null) accessToken = localStorage.getItem(ACCESS_KEY);
  if (refreshToken === null) refreshToken = localStorage.getItem(REFRESH_KEY);
}

export function setTokens(access: string, refresh?: string) {
  accessToken = access;
  if (typeof window !== "undefined") localStorage.setItem(ACCESS_KEY, access);
  if (refresh !== undefined) {
    refreshToken = refresh;
    if (typeof window !== "undefined")
      localStorage.setItem(REFRESH_KEY, refresh);
  }
}

export function clearTokens() {
  accessToken = null;
  refreshToken = null;
  if (typeof window !== "undefined") {
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(REFRESH_KEY);
  }
}

export function getAccessToken(): string | null {
  hydrate();
  return accessToken;
}

export const api = axios.create({
  baseURL: "/api",
  withCredentials: true,
});

// Attach the bearer token to every outgoing request.
api.interceptors.request.use((config) => {
  const token = getAccessToken();
  if (token) {
    config.headers = config.headers ?? {};
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (r) => r,
  async (err) => {
    const original = err.config as AxiosRequestConfig & { _retried?: boolean };
    if (err.response?.status === 401 && original && !original._retried) {
      original._retried = true;
      hydrate();
      // Try a silent refresh using the stored refresh token.
      try {
        const { data } = await axios.post(
          "/api/v1/auth/refresh",
          refreshToken ? { refresh_token: refreshToken } : {},
          { withCredentials: true },
        );
        if (data?.access_token) {
          setTokens(data.access_token, data.refresh_token);
        }
        original.headers = original.headers ?? {};
        if (accessToken)
          original.headers.Authorization = `Bearer ${accessToken}`;
        return api.request(original);
      } catch {
        clearTokens();
        if (typeof window !== "undefined") {
          window.location.href = "/login";
        }
      }
    }
    return Promise.reject(err);
  },
);
