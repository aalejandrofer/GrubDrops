/** The account whose login flow is open in Settings. The header skips its needs-login problem. */
export const loginFlow = $state<{ id: string | null }>({ id: null });
