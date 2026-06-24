import type { DashboardSnapshot, ApiErrorEnvelope, DropsPage } from './types';
import { readCookie } from './csrf';

// ApiError carries the server error envelope's stable code plus the HTTP
// status, so SPA views can branch on the failure kind.
export class ApiError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

// apiFetch GETs a JSON resource. On 401 it redirects to /login (session
// expired) and returns a never-resolving promise so callers do not render
// an error flash while the page navigates away. On other non-2xx it parses
// the error envelope and throws ApiError. On success it returns the parsed
// body.
export async function apiFetch<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: 'include' });

  if (res.status === 401) {
    window.location.assign('/login');
    return new Promise<T>(() => {});
  }

  if (!res.ok) {
    let code = 'internal';
    let message = res.statusText || `request failed (${res.status})`;
    try {
      const body = (await res.json()) as ApiErrorEnvelope;
      if (body?.error?.code) {
        code = body.error.code;
        message = body.error.message;
      }
    } catch {
      // non-JSON error body; keep the status-derived defaults
    }
    throw new ApiError(code, message, res.status);
  }

  return (await res.json()) as T;
}

export function fetchDashboard(): Promise<DashboardSnapshot> {
  return apiFetch<DashboardSnapshot>('/api/dashboard');
}

export function fetchDrops(tab: string): Promise<DropsPage> {
  return apiFetch<DropsPage>('/api/drops?tab=' + encodeURIComponent(tab));
}

// apiSend performs a mutating request: it attaches the CSRF token (read from
// the csrftoken cookie) in the X-CSRF-Token header, posts JSON, and shares
// apiFetch's 401-redirect / ApiError handling.
export async function apiSend<T>(path: string, method: 'POST' | 'PUT' | 'DELETE', body?: unknown): Promise<T> {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = readCookie('csrftoken');
  if (token) headers['X-CSRF-Token'] = token;

  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (res.status === 401) {
    window.location.assign('/login');
    return new Promise<T>(() => {});
  }
  if (!res.ok) {
    let code = 'internal';
    let message = res.statusText || `request failed (${res.status})`;
    try {
      const env = (await res.json()) as ApiErrorEnvelope;
      if (env?.error?.code) { code = env.error.code; message = env.error.message; }
    } catch { /* keep defaults */ }
    throw new ApiError(code, message, res.status);
  }
  // 200 with a JSON body ({ok:true}) or empty.
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}
