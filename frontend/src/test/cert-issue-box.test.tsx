import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import CertIssueBox from '@/pages/inbounds/CertIssueBox';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const getSpy = vi.mocked(HttpUtil.get);
const postSpy = vi.mocked(HttpUtil.post);

const running = { domain: 'hk.example.com', running: true, done: false, error: '', step: 'issue' };

beforeEach(() => {
  getSpy.mockReset();
  postSpy.mockReset();
});

describe('CertIssueBox', () => {
  it('requests the certificate for the typed domain and reports back once it is installed', async () => {
    const onIssued = vi.fn();
    postSpy.mockResolvedValue(new Msg(true, '', running));
    getSpy.mockResolvedValue(
      new Msg(true, '', { ...running, running: false, done: true, step: 'done', panel: false }),
    );
    renderWithProviders(<CertIssueBox domain="" onIssued={onIssued} />);
    fireEvent.change(screen.getByPlaceholderText('hk.example.com'), {
      target: { value: ' hk.example.com ' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Request certificate/ }));

    await waitFor(() => expect(onIssued).toHaveBeenCalledTimes(1), { timeout: 6000 });
    expect(postSpy.mock.calls[0][1]).toEqual({ domain: 'hk.example.com', usePanel: false });
    expect(
      screen.getByText('Certificate for hk.example.com installed; the TLS presets can use it now.'),
    ).toBeTruthy();
  });

  it('shows the acme.sh log when the request fails, and does not report success', async () => {
    const onIssued = vi.fn();
    postSpy.mockResolvedValue(new Msg(true, '', running));
    getSpy.mockResolvedValue(
      new Msg(true, '', {
        ...running,
        running: false,
        error: 'acme.sh --issue failed (exit status 1); the log below says why',
        log: 'invalid domain: zone not found in this token',
      }),
    );
    renderWithProviders(<CertIssueBox domain="hk.example.com" onIssued={onIssued} />);
    fireEvent.click(screen.getByRole('button', { name: /Request certificate/ }));

    await waitFor(
      () => expect(screen.getByText('invalid domain: zone not found in this token')).toBeTruthy(),
      { timeout: 6000 },
    );
    expect(onIssued).not.toHaveBeenCalled();
  });
});

describe('CertIssueBox on a node', () => {
  it('asks the node through the master and never offers to change the node panel certificate', async () => {
    const onIssued = vi.fn();
    postSpy.mockImplementation(async (url: string) => {
      if (url === '/panel/api/protect/nodes/4/certIssue') return new Msg(true, '', running);
      return new Msg(true, '', { ...running, running: false, done: true, step: 'done' });
    });
    renderWithProviders(<CertIssueBox domain="hk.example.com" nodeId={4} onIssued={onIssued} />);
    expect(screen.queryByText('Also use it for the panel (HTTPS)')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Request certificate/ }));

    await waitFor(() => expect(onIssued).toHaveBeenCalledTimes(1), { timeout: 6000 });
    expect(postSpy.mock.calls.map((c) => [c[0], c[1]])).toEqual([
      ['/panel/api/protect/nodes/4/certIssue', { domain: 'hk.example.com' }],
      ['/panel/api/protect/nodes/4/certStatus', {}],
    ]);
    expect(getSpy).not.toHaveBeenCalled();
  });
});
