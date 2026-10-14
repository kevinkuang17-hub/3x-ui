import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import DiagnoseModal from '@/pages/inbounds/DiagnoseModal';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const postSpy = vi.mocked(HttpUtil.post);

const broken = {
  inboundId: 7,
  remark: 'US A Hy2',
  protocol: 'hysteria',
  port: 443,
  nodeId: 2,
  address: 'us.example.com',
  ok: false,
  delayMs: 0,
  findings: [
    {
      key: 'firewallBlocked',
      level: 'error',
      params: { port: 443, proto: 'UDP' },
      fix: 'firewall',
    },
    { key: 'e2eFailed', level: 'error', params: { error: 'timeout' } },
  ],
};
const fixed = {
  ...broken,
  ok: true,
  delayMs: 210,
  findings: [{ key: 'e2eOk', level: 'ok', params: { delay: 210 } }],
};

beforeEach(() => {
  postSpy.mockReset();
});

describe('DiagnoseModal', () => {
  it('explains the problem and fixes the node firewall in one click', async () => {
    let calls = 0;
    postSpy.mockImplementation(async (url: string) => {
      if (url === '/panel/api/inbounds/diagnose')
        return new Msg(true, '', [calls++ ? fixed : broken]);
      return new Msg(true, '', {});
    });
    const onChanged = vi.fn();
    renderWithProviders(
      <MemoryRouter>
        <DiagnoseModal open ids={[]} onClose={vi.fn()} onChanged={onChanged} />
      </MemoryRouter>,
    );

    expect(await screen.findByText('The firewall (ufw) blocks UDP 443')).toBeTruthy();
    expect(screen.getByText('1 of 1 inbounds have problems')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Open ports in the firewall' }));

    await waitFor(() => expect(screen.getByText('Works · 210 ms')).toBeTruthy());
    const urls = postSpy.mock.calls.map((c) => c[0]);
    expect(urls).toContain('/panel/api/protect/nodes/2/firewallAllow');
    const again = postSpy.mock.calls.filter((c) => c[0] === '/panel/api/inbounds/diagnose');
    expect(again[1][1]).toEqual({ ids: [7] });
    expect(onChanged).toHaveBeenCalledTimes(1);
  });
});
