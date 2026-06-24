import { expect, test } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import LiveChannels from './LiveChannels.svelte';

test('renders a channel card linking to URL', () => {
  render(LiveChannels, { props: { channels: [
    { Login: 'streamer', Platform: 'kick', URL: 'https://kick.com/streamer', Initial: 'S', Game: 'GameX', Campaign: 'Camp', Views: '62.4k', ViewerN: 62400 },
  ] } });
  const link = screen.getByText('streamer').closest('a') as HTMLAnchorElement;
  expect(link.getAttribute('href')).toBe('https://kick.com/streamer');
  expect(screen.getByText('62.4k')).toBeTruthy();
});
