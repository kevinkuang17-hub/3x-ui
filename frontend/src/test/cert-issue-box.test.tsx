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
