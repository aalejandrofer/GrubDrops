import { expect, test } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import NextClaims from './NextClaims.svelte';

test('renders claim entries', () => {
  render(NextClaims, { props: { claims: [
    { ID: 'a1', Name: 'acc', Platform: 'twitch', State: 'watching', StateSub: '', Channel: 'c', DropName: 'Helmet', DropPercent: 80, DropETA: '00:12', Enabled: true },
  ] } });
  expect(screen.getByText('Helmet')).toBeTruthy();
  expect(screen.getByText('00:12')).toBeTruthy();
});
