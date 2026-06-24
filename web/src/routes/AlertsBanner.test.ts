import { expect, test } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import AlertsBanner from './AlertsBanner.svelte';

test('renders an alert CTA', () => {
  render(AlertsBanner, { props: { alerts: [{ Kind: 'needs_auth', Account: '@a', URL: '/x', Action: 'Re-auth' }] } });
  expect(screen.getByText('Re-auth')).toBeTruthy();
  expect((screen.getByText('Re-auth') as HTMLAnchorElement).getAttribute('href')).toBe('/x');
});

test('renders nothing when empty', () => {
  const { container } = render(AlertsBanner, { props: { alerts: null } });
  expect(container.querySelector('.alert')).toBeNull();
});
