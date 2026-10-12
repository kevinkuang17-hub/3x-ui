import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import QuickInboundModal from '@/pages/inbounds/QuickInboundModal';
import { HttpUtil, Msg } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';

import { renderWithProviders } from './test-utils';

const getSpy = vi.mocked(HttpUtil.get);
const postSpy = vi.mocked(HttpUtil.post);

const CLIENTS = [
  { email: 'alice', group: 'sales', enable: true },
  { email: 'bob', group: 'sales', enable: true },
  { email: 'carol', group: 'ops', enable: true },
];

function recommendation(over: Record<string, unknown> = {}) {
  return {
    domain: 'hk.example.com',
    domainIps: ['203.0.113.5'],
    serverIps: ['203.0.113.5'],
    pointsHere: true,
    certFile: '/root/cert/hk.example.com/fullchain.pem',
    keyFile: '/root/cert/hk.example.com/privkey.pem',
    certState: 'ok',
    certNotAfter: Date.UTC(2027, 0, 1),
    port: 443,
    preferredPortBusy: false,
    recommended: 'tls',
    ...over,
  };
}

function mockServer(rec: Record<string, unknown>) {
  getSpy.mockImplementation(async (url: string) => {
    if (url.startsWith('/panel/api/inbounds/recommend')) return new Msg(true, '', rec);
    if (url === '/panel/api/clients/list') return new Msg(true, '', CLIENTS);
    if (url === '/panel/api/server/getNewX25519Cert')
      return new Msg(true, '', { privateKey: 'priv-key', publicKey: 'pub-key' });
    return new Msg(true, '', {});
  });
  postSpy.mockImplementation(async (url: string) => {
    if (url === '/panel/api/inbounds/add') return new Msg(true, '', { id: 42 });
    if (url === '/panel/api/server/scanRealityTargets')
      return new Msg(true, '', [
        { target: 'bad.example:443', feasible: false, serverNames: ['bad.example'] },
        { target: 'www.apple.com:443', feasible: true, serverNames: ['www.apple.com'] },
      ]);
    return new Msg(true, '', {});
  });
}

function renderModal(onCreated = vi.fn(), onClose = vi.fn()) {
  renderWithProviders(
    <QuickInboundModal
      open
      nodes={[] as NodeRecord[]}
      onClose={onClose}
      onCreated={onCreated}
      onAdvanced={vi.fn()}
    />,
  );
  return { onCreated, onClose };
}

const call = (url: string) => postSpy.mock.calls.find((c) => c[0] === url);

beforeEach(() => {
  getSpy.mockReset();
  postSpy.mockReset();
});

describe('QuickInboundModal', () => {
  it('creates the recommended TLS inbound on the panel certificate and attaches every client', async () => {
    mockServer(recommendation());
    const { onCreated } = renderModal();
    const create = await screen.findByRole('button', { name: 'Create' });
    await waitFor(() => expect((create as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(create);

    await waitFor(() => expect(call('/panel/api/clients/bulkAttach')).toBeTruthy());
    const body = call('/panel/api/inbounds/add')![1] as Record<string, unknown>;
    const stream = JSON.parse(body.streamSettings as string);
    expect(body.port).toBe(443);
    expect(stream.security).toBe('tls');
    expect(stream.tlsSettings.serverName).toBe('hk.example.com');
    expect(stream.tlsSettings.certificates[0].certificateFile).toBe(
      '/root/cert/hk.example.com/fullchain.pem',
    );
    expect(call('/panel/api/clients/bulkAttach')![1]).toEqual({
      emails: ['alice', 'bob', 'carol'],
      inboundIds: [42],
    });
    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
  });

  it('falls back to Reality with the first feasible scanned site when the certificate is missing', async () => {
    mockServer(
      recommendation({ certState: 'missing', certFile: '', keyFile: '', recommended: 'reality' }),
    );
    renderModal();
    expect(await screen.findByText('www.apple.com')).toBeTruthy();
    expect((screen.getByRole('radio', { name: /VLESS \+ TLS/ }) as HTMLInputElement).disabled).toBe(
      true,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));

    await waitFor(() => expect(call('/panel/api/inbounds/add')).toBeTruthy());
    const stream = JSON.parse(
      (call('/panel/api/inbounds/add')![1] as Record<string, unknown>).streamSettings as string,
    );
    expect(stream.security).toBe('reality');
    expect(stream.realitySettings.target).toBe('www.apple.com:443');
    expect(stream.realitySettings.privateKey).toBe('priv-key');
    expect(stream.realitySettings.settings.publicKey).toBe('pub-key');
  });
});
