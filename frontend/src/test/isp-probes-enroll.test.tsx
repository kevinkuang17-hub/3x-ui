import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';

import IspProbesCard from '@/pages/nodes/IspProbesCard';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

describe('IspProbesCard enrollment', () => {
  it('shows one command for every router, on the subscription domain first', async () => {
    vi.mocked(HttpUtil.get).mockImplementation(async (url: string) => {
      if (url === '/panel/api/probes/enroll')
        return new Msg(true, '', {
          path: '/probe-enroll/abc',
          subUrl: 'https://sub.example.com/probe-enroll/abc',
        });
      return new Msg(true, '', { probes: [], targets: [], results: [], verdicts: {} });
    });
    renderWithProviders(<IspProbesCard />);
    fireEvent.click(await screen.findByRole('button', { name: /One command for all routers/ }));
    expect(
      await screen.findByText(
        "curl -fsSL 'https://sub.example.com/probe-enroll/abc/agent.sh' | sh -s -- enroll 'https://sub.example.com/probe-enroll/abc'",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText(
        `curl -fsSL '${window.location.origin}/probe-enroll/abc/agent.sh' | sh -s -- enroll '${window.location.origin}/probe-enroll/abc'`,
      ),
    ).toBeTruthy();
  });
});
