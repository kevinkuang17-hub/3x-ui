import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import { CloudflareCard } from '@/pages/protect/ProtectPage';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const getSpy = vi.mocked(HttpUtil.get);
const postSpy = vi.mocked(HttpUtil.post);

const cfInfo = {
  panelDomain: 'panel.example.com',
  panelPort: 2053,
  subDomain: '',
  subPort: 2096,
  subEnable: false,
  cfConfigured: true,
};
const proxied = {
  domain: 'panel.example.com',
  found: true,
  ip: '203.0.113.7',
  proxied: true,
  port: 2053,
  portOk: true,
  sslMode: 'strict',
};

function serve(cfOnly: object) {
  getSpy.mockImplementation(async (url: string) => {
    if (url === '/panel/api/protect/cf') return new Msg(true, '', cfInfo);
    if (url === '/panel/api/protect/cf/only') return new Msg(true, '', cfOnly);
    return new Msg(true, '', {});
  });
  postSpy.mockImplementation(async (url: string, body?: unknown) => {
    if (url === '/panel/api/protect/cf/status') return new Msg(true, '', proxied);
    if (url === '/panel/api/protect/cf/only') {
      const on = (body as { on: boolean }).on;
      return new Msg(true, '', { ...cfOnly, ports: on ? [2053] : [] });
    }
    return new Msg(true, '', {});
  });
}

const denyNothingLimited = {
  ports: [],
  firewall: 'ufw',
  active: true,
  defaultDeny: true,
  ranges: 0,
  updatedAt: 0,
};

beforeEach(() => {
  getSpy.mockReset();
  postSpy.mockReset();
});

describe('Cloudflare only', () => {
  it('limits the panel port once its domain is behind Cloudflare, marking it as the panel', async () => {
    serve(denyNothingLimited);
    renderWithProviders(<CloudflareCard />);
    expect(screen.queryByRole('switch', { name: 'Cloudflare only' })).toBeNull();
    fireEvent.click(await screen.findByRole('button', { name: /Check/ }));

    const toggle = await screen.findByRole('switch', { name: 'Cloudflare only' });
    fireEvent.click(toggle);

    await waitFor(() =>
      expect(postSpy).toHaveBeenCalledWith(
        '/panel/api/protect/cf/only',
        { port: 2053, on: true, panel: true },
        expect.anything(),
      ),
    );
    await waitFor(() => expect(toggle.getAttribute('aria-checked')).toBe('true'));
    expect(screen.getByText(/ufw allow 2053\/tcp/)).toBeTruthy();
  });

  it('will not limit a port while ufw lets every connection in', async () => {
    serve({ ...denyNothingLimited, defaultDeny: false });
    renderWithProviders(<CloudflareCard />);
    expect(await screen.findByText(/ufw default deny incoming/)).toBeTruthy();
    fireEvent.click(await screen.findByRole('button', { name: /Check/ }));

    const toggle = await screen.findByRole('switch', { name: 'Cloudflare only' });
    expect(toggle.hasAttribute('disabled')).toBe(true);
  });

  it('shows a limited port as limited before any Cloudflare check, so it can be reopened', async () => {
    serve({ ...denyNothingLimited, ports: [2053], ranges: 22 });
    renderWithProviders(<CloudflareCard />);
    const toggle = await screen.findByRole('switch', { name: 'Cloudflare only' });
    expect(toggle.getAttribute('aria-checked')).toBe('true');
    fireEvent.click(toggle);
    await waitFor(() =>
      expect(postSpy).toHaveBeenCalledWith(
        '/panel/api/protect/cf/only',
        { port: 2053, on: false, panel: true },
        expect.anything(),
      ),
    );
  });
});
