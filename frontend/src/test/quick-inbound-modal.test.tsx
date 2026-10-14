import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import QuickInboundModal from '@/pages/inbounds/QuickInboundModal';
import { HttpUtil, Msg } from '@/utils';
import type { NodeRecord } from '@/api/queries/useNodesQuery';
import type { ExistingInbound } from '@/lib/xray/inbound-presets';

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
    tcpPorts: [443, 8443, 2053, 2083],
    udpPorts: [443, 40000],
    recommended: 'tls',
    ...over,
  };
}

function mockServer(rec: Record<string, unknown>) {
  let nextId = 40;
  getSpy.mockImplementation(async (url: string) => {
    if (url.startsWith('/panel/api/inbounds/recommend')) return new Msg(true, '', rec);
    if (url === '/panel/api/clients/list') return new Msg(true, '', CLIENTS);
    if (url === '/panel/api/protect/decoy/page') return new Msg(true, '', '<html>Star Tech</html>');
    if (url === '/panel/api/server/getNewX25519Cert')
      return new Msg(true, '', { privateKey: 'priv-key', publicKey: 'pub-key' });
    return new Msg(true, '', {});
  });
  postSpy.mockImplementation(async (url: string) => {
    if (url === '/panel/api/inbounds/add') return new Msg(true, '', { id: ++nextId });
    if (url === '/panel/api/server/scanRealityTargets')
      return new Msg(true, '', [
        { target: 'bad.example:443', feasible: false, serverNames: ['bad.example'] },
        { target: 'h1.example:443', feasible: true, h2: false, serverNames: ['h1.example'] },
        { target: 'www.apple.com:443', feasible: true, h2: true, serverNames: ['www.apple.com'] },
      ]);
    return new Msg(true, '', {});
  });
}

function renderModal(inbounds: ExistingInbound[] = [], onCreated = vi.fn()) {
  renderWithProviders(
    <QuickInboundModal
      open
      nodes={[] as NodeRecord[]}
      inbounds={inbounds}
      onClose={vi.fn()}
      onCreated={onCreated}
      onAdvanced={vi.fn()}
    />,
  );
  return { onCreated };
}

const added = () =>
  postSpy.mock.calls
    .filter((c) => c[0] === '/panel/api/inbounds/add')
    .map((c) => {
      const body = c[1] as {
        protocol: string;
        port: number;
        remark: string;
        streamSettings: string;
      };
      return { ...body, stream: JSON.parse(body.streamSettings) };
    });

const presetBox = (id: string) =>
  document.querySelector(`[data-preset="${id}"] input[type="checkbox"]`) as HTMLInputElement;

async function createButton() {
  const button = (await screen.findByRole('button', { name: /^Create/ })) as HTMLButtonElement;
  await waitFor(() => expect(button.disabled).toBe(false));
  return button;
}

beforeEach(() => {
  getSpy.mockReset();
  postSpy.mockReset();
});

describe('QuickInboundModal', () => {
  it('creates the recommended set, skipping a protocol the server already has', async () => {
    mockServer(recommendation());
    const existing: ExistingInbound[] = [
      {
        id: 7,
        remark: 'Old Reality',
        port: 8443,
        protocol: 'vless',
        nodeId: null,
        streamSettings: { network: 'tcp', security: 'reality' },
      },
    ];
    const { onCreated } = renderModal(existing);
    fireEvent.click(await createButton());

    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    expect(presetBox('reality-vision').disabled).toBe(true);
    const bodies = added();
    expect(bodies.map((b) => [b.protocol, b.stream.security, b.port, b.remark])).toEqual([
      ['vless', 'tls', 443, 'TLS'],
      ['hysteria', 'tls', 443, 'Hy2'],
    ]);
    const attach = postSpy.mock.calls.find((c) => c[0] === '/panel/api/clients/bulkAttach');
    expect(attach?.[1]).toEqual({ emails: ['alice', 'bob', 'carol'], inboundIds: [41, 42] });
  });

  it('offers only REALITY without a certificate and picks an HTTP/2 target', async () => {
    mockServer(
      recommendation({ certState: 'missing', certFile: '', keyFile: '', recommended: 'reality' }),
    );
    renderModal();
    expect(await screen.findByText('www.apple.com')).toBeTruthy();
    for (const id of ['tls-vision', 'trojan-tls', 'ws-cdn', 'hysteria2'])
      expect(presetBox(id).disabled).toBe(true);
    fireEvent.click(await createButton());

    await waitFor(() => expect(added()).toHaveLength(2));
    const [vision, xhttp] = added();
    expect([vision.stream.network, vision.port]).toEqual(['tcp', 443]);
    expect([xhttp.stream.network, xhttp.port]).toEqual(['xhttp', 8443]);
    expect(xhttp.stream.realitySettings.target).toBe('www.apple.com:443');
  });

  it('refuses to create an inbound whose remark already exists', async () => {
    mockServer(recommendation());
    renderModal([
      { id: 9, remark: 'HK', port: 1000, protocol: 'vmess', nodeId: null, streamSettings: {} },
    ]);
    await createButton();
    const remark = document.querySelector(
      '[data-preset="tls-vision"] .ant-input-affix-wrapper input',
    ) as HTMLInputElement;
    fireEvent.change(remark, { target: { value: 'HK' } });

    expect(await screen.findByText('Remark already exists: HK')).toBeTruthy();
    const button = screen.getByRole('button', { name: /^Create/ }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });

  it('opens the new ports in ufw and gives Hysteria2 the decoy website', async () => {
    mockServer(recommendation({ firewall: 'ufw' }));
    const { onCreated } = renderModal();
    fireEvent.click(await createButton());

    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    const hy = added().find((b) => b.protocol === 'hysteria')!;
    expect(hy.stream.hysteriaSettings.masquerade.content).toBe('<html>Star Tech</html>');
    const allow = postSpy.mock.calls.find((c) => c[0] === '/panel/api/protect/firewall/allow');
    expect(allow?.[1]).toEqual({
      rules: [
        { port: 443, proto: 'tcp' },
        { port: 8443, proto: 'tcp' },
        { port: 443, proto: 'udp' },
      ],
    });
  });

  it('fronts the panel website with REALITY when it already runs behind REALITY', async () => {
    mockServer(
      recommendation({
        certState: 'missing',
        certFile: '',
        keyFile: '',
        recommended: 'reality',
        realityTarget: '127.0.0.1:24873',
      }),
    );
    const { onCreated } = renderModal();
    fireEvent.click(await createButton());

    await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(1));
    const vision = added().find((b) => b.stream.network === 'tcp')!;
    expect(vision.stream.realitySettings.target).toBe('127.0.0.1:24873');
    expect(vision.stream.realitySettings.serverNames).toEqual(['hk.example.com']);
    expect(vision.stream.realitySettings.xver).toBe(1);
    expect(postSpy.mock.calls.some((c) => c[0] === '/panel/api/server/scanRealityTargets')).toBe(
      false,
    );
  });
});
