import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import RealitySelfCard from '@/pages/protect/RealitySelfCard';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const getSpy = vi.mocked(HttpUtil.get);
const postSpy = vi.mocked(HttpUtil.post);

const frontBefore = {
  enabled: true,
  frontOnly: false,
  hasCert: true,
  conflict: '',
  port: 443,
  panelPort: 2096,
  loopback: false,
  running: { listening: true, frontOnly: false, error: '', addr: '[::]:443' },
};
const frontAfter = {
  ...frontBefore,
  port: 24873,
  loopback: true,
  running: { listening: true, frontOnly: false, error: '', addr: '127.0.0.1:24873' },
};

beforeEach(() => {
  getSpy.mockReset();
  postSpy.mockReset();
});

describe('RealitySelfCard', () => {
  it('moves REALITY to 443 only after the restarted panel serves the website inside', async () => {
    let prepared = false;
    getSpy.mockImplementation(async (url: string) => {
      if (url === '/panel/api/protect/front')
        return new Msg(true, '', prepared ? frontAfter : frontBefore);
      if (url === '/panel/api/inbounds/list/slim')
        return new Msg(true, '', [
          {
            id: 3,
            remark: 'TLS',
            port: 8443,
            protocol: 'vless',
            streamSettings: { security: 'tls' },
          },
          {
            id: 5,
            remark: 'Reality',
            port: 2053,
            protocol: 'vless',
            streamSettings: { security: 'reality' },
          },
        ]);
      return new Msg(true, '', {});
    });
    postSpy.mockImplementation(async (url: string) => {
      if (url === '/panel/api/protect/realitySelf/prepare') {
        prepared = true;
        return new Msg(true, '', { domain: 'us.example.com', internalPort: 24873 });
      }
      return new Msg(true, '', { id: 5, port: 443 });
    });
    renderWithProviders(<RealitySelfCard />);
    const button = await screen.findByRole('button', { name: /Set up/ });
    await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(button);

    await waitFor(
      () =>
        expect(
          screen.getByText('Done: REALITY now runs on 443 in front of https://us.example.com/'),
        ).toBeTruthy(),
      { timeout: 5000 },
    );
    const urls = postSpy.mock.calls.map((c) => c[0]);
    expect(urls).toEqual([
      '/panel/api/protect/realitySelf/prepare',
      '/panel/api/protect/realitySelf/apply',
    ]);
    expect(postSpy.mock.calls[1][1]).toEqual({ inboundId: 5 });
  }, 10000);
});
