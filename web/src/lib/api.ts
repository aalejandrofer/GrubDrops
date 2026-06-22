import type { DashboardSnapshot } from './types';

export async function fetchDashboard(): Promise<DashboardSnapshot> {
  const res = await fetch('/api/dashboard', { credentials: 'include' });
  if (!res.ok) {
    throw new Error(`/api/dashboard returned ${res.status}`);
  }
  return res.json() as Promise<DashboardSnapshot>;
}
