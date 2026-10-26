import { error } from '@sveltejs/kit';

// Mockups live in the repo but never ship: release builds answer 404.
export function load() {
	if (!import.meta.env.DEV) error(404, 'Not found');
}
