import type { DashboardSnapshot, ApiErrorEnvelope } from './types';

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
