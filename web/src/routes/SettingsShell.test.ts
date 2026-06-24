import { expect, test } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import SettingsShell from './SettingsShell.svelte';

test('renders subnav + active tab content', () => {
  const children = createRawSnippet(() => ({ render: () => '<p>TABBODY</p>' }));
  render(SettingsShell, { props: { active: 'general', children } });
  expect(screen.getByText('TABBODY')).toBeTruthy();
  expect(screen.getByText(/notifications/i)).toBeTruthy(); // a subnav link
});
