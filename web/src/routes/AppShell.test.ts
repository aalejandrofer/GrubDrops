import { expect, test } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import AppShell from './AppShell.svelte';

test('renders nav links and page content', () => {
  const children = createRawSnippet(() => ({ render: () => '<p>PAGE</p>' }));
  render(AppShell, { props: { children } });
  expect(screen.getByText('PAGE')).toBeTruthy();
  expect(screen.getAllByText(/drops/i).length).toBeGreaterThan(0);
  expect(screen.getByText(/settings/i)).toBeTruthy();
});
